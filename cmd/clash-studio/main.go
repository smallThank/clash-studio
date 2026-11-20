package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"clash-rule-gen/internal/studio"

	_ "github.com/mattn/go-sqlite3"
)

type settings struct {
	address   string
	dataDir   string
	frontDir  string
	publicURL string
	devOrigin string
	masterKey string
}

func readSettings() (settings, error) {
	s := settings{
		address:   envOr("ADDR", "127.0.0.1:8080"),
		dataDir:   os.Getenv("DATA_DIR"),
		frontDir:  envOr("FRONTEND_DIR", "frontend/dist"),
		publicURL: strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")),
		devOrigin: os.Getenv("DEV_ORIGIN"),
		masterKey: os.Getenv("CLASH_MASTER_KEY"),
	}
	if s.dataDir == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return s, fmt.Errorf("locate data directory: %w", err)
		}
		s.dataDir = filepath.Join(configDir, "clash-config-studio")
	}
	var err error
	if s.publicURL != "" {
		s.publicURL, err = origin(s.publicURL)
		if err != nil {
			return s, fmt.Errorf("PUBLIC_BASE_URL: %w", err)
		}
		parsed, _ := url.Parse(s.publicURL)
		if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsUnspecified() {
			s.publicURL = ""
		}
	}
	if s.devOrigin != "" {
		s.devOrigin, err = origin(s.devOrigin)
		if err != nil {
			return s, fmt.Errorf("DEV_ORIGIN: %w", err)
		}
	}
	return s, nil
}

func origin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", errors.New("use an HTTP(S) origin without a path, credentials, query, or fragment")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func loadMasterKey(dir, override string) ([]byte, error) {
	if override != "" {
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if data, err := encoding.DecodeString(override); err == nil && len(data) == 32 {
				return data, nil
			}
		}
		return nil, errors.New("CLASH_MASTER_KEY must encode exactly 32 bytes in Base64")
	}
	path := filepath.Join(dir, "master.key")
	key, err := os.ReadFile(path)
	if !errors.Is(err, os.ErrNotExist) {
		return checkKey(key, err)
	}

	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, ".master-key-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(key); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Link(tmp.Name(), path); errors.Is(err, os.ErrExist) {
		return checkKey(os.ReadFile(path))
	} else if err != nil {
		return nil, err
	}
	return key, nil
}

func checkKey(key []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("master.key must contain exactly 32 bytes")
	}
	return key, nil
}

func openDB(path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{
		Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&_txlock=immediate",
	}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func siteHandler(api http.Handler, frontend fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/s/", api)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	files := http.FileServerFS(frontend)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		info, err := fs.Stat(frontend, name)
		if err != nil || info.IsDir() {
			if r.URL.Path == "/" && errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "前端尚未构建，请在 frontend 目录运行 npm run build。", http.StatusServiceUnavailable)
				return
			}
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}

func run() error {
	s, err := readSettings()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	key, err := loadMasterKey(s.dataDir, s.masterKey)
	if err != nil {
		return fmt.Errorf("load encryption key: %w", err)
	}
	db, err := openDB(filepath.Join(s.dataDir, "studio.sqlite"))
	if err != nil {
		return fmt.Errorf("open SQLite: %w", err)
	}
	defer db.Close()
	api, err := studio.New(db, studio.Options{
		MasterKey: key, PublicBaseURL: s.publicURL,
		SecureCookies: strings.HasPrefix(s.publicURL, "https://"), AllowedOrigin: s.devOrigin,
	})
	if err != nil {
		return fmt.Errorf("initialize service: %w", err)
	}
	server := &http.Server{
		Addr: s.address, Handler: siteHandler(api, os.DirFS(s.frontDir)),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second,
		WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	log.Printf("Clash 配置台监听 %s", s.address)
	if s.publicURL != "" {
		log.Printf("订阅公开地址 %s", s.publicURL)
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
