package studio

import (
	"context"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"clash-rule-gen/internal/config"
)

// Options configures the HTTP service and its authenticated subscription imports.
type Options struct {
	MasterKey     []byte
	PublicBaseURL string
	SecureCookies bool
	AllowedOrigin string
	Fetch         func(context.Context, string, string) (string, error)
}

type Server struct {
	db           *sql.DB
	aead         cipher.AEAD
	csrfKey      []byte
	publicBase   string
	publicOrigin string
	devOrigin    string
	secureCookie bool
	fetch        func(context.Context, string, string) (string, error)
	passwordWork chan struct{}
	limits       rateLimits
}

type apiError struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Issues  []config.Issue `json:"issues,omitempty"`
}

func (e *apiError) Error() string { return e.Message }

func badRequest(message string) error {
	return &apiError{Status: http.StatusBadRequest, Code: "invalid_request", Message: message}
}

func invalidConfig(message string, issues []config.Issue) error {
	return &apiError{Status: http.StatusUnprocessableEntity, Code: "invalid_config", Message: message, Issues: issues}
}

func notFound() error {
	return &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "该资源不存在。"}
}

func conflict() error {
	return &apiError{Status: http.StatusConflict, Code: "revision_conflict", Message: "草稿已更新，请重新加载后再试。"}
}

func throttled() error {
	return &apiError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "请求过于频繁，请稍后再试。"}
}

func unauthenticated() error {
	return &apiError{Status: http.StatusUnauthorized, Code: "unauthenticated", Message: "请先登录。"}
}

func sameOriginRequired() error {
	return &apiError{Status: http.StatusForbidden, Code: "invalid_origin", Message: "请从应用所在的站点提交。"}
}

func New(db *sql.DB, opts Options) (http.Handler, error) {
	if db == nil {
		return nil, errors.New("SQLite database is required")
	}
	aead, err := newAEAD(opts.MasterKey)
	if err != nil {
		return nil, err
	}
	base, origin, err := configuredURL(opts.PublicBaseURL, true)
	if err != nil {
		return nil, err
	}
	_, devOrigin, err := configuredURL(opts.AllowedOrigin, false)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	h := hmac.New(sha256.New, opts.MasterKey)
	h.Write([]byte("clash-studio/session-csrf/v1"))
	server := &Server{
		db: db, aead: aead, csrfKey: h.Sum(nil), publicBase: base, publicOrigin: origin,
		devOrigin: devOrigin, secureCookie: opts.SecureCookies, fetch: importFetcher(opts.Fetch),
		passwordWork: make(chan struct{}, 4),
	}
	if err := server.migrate(ctx); err != nil {
		return nil, err
	}
	return server.routes(), nil
}

func configuredURL(raw string, allowPath bool) (base, origin string, err error) {
	if raw == "" {
		return "", "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Opaque != "" ||
		(u.Scheme != "http" && u.Scheme != "https") || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" ||
		(!allowPath && u.Path != "" && u.Path != "/") {
		return "", "", errors.New("the HTTP origin or public base URL is invalid")
	}
	return strings.TrimRight(u.String(), "/"), originOf(u), nil
}

func originOf(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" || (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	} else {
		host = net.JoinHostPort(host, port)
	}
	return u.Scheme + "://" + host
}

func requestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return originOf(&url.URL{Scheme: scheme, Host: r.Host})
}

func (s *Server) sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	raw := r.Header.Get("Origin")
	if raw == "" {
		// Client calls without Origin still need the session CSRF header.
		return true
	}
	_, origin, err := configuredURL(raw, false)
	if err != nil {
		return false
	}
	return origin == s.publicOrigin || origin == s.devOrigin || origin == requestBase(r) ||
		sameOriginHTTPSProxy(r, origin)
}

func sameOriginHTTPSProxy(r *http.Request, origin string) bool {
	// The browser reports same-origin while TLS may terminate before this server.
	// Require the proxy to preserve the original Host, without trusting forwarded headers.
	return r.TLS == nil && r.Header.Get("Sec-Fetch-Site") == "same-origin" &&
		origin == originOf(&url.URL{Scheme: "https", Host: r.Host})
}

func (s *Server) useSecureCookie(r *http.Request) bool {
	_, origin, _ := configuredURL(r.Header.Get("Origin"), false)
	return s.secureCookie || r.TLS != nil || sameOriginHTTPSProxy(r, origin)
}

type apiHandler func(http.ResponseWriter, *http.Request) error
type userHandler func(http.ResponseWriter, *http.Request, sessionUser) error

func (s *Server) api(fn apiHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err := fn(w, r); err != nil {
			writeAPIError(w, err)
		}
	}
}

func parseBody(w http.ResponseWriter, r *http.Request, dest any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &apiError{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media_type", Message: "请求正文需要使用 JSON。"}
	}
	const requestLimit = 8 << 20
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			return &apiError{Status: http.StatusRequestEntityTooLarge, Code: "request_too_large", Message: "请求内容过大。"}
		}
		return badRequest("JSON 字段或格式无效。")
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return badRequest("请求需要一个 JSON 对象。")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeAPIError(w http.ResponseWriter, err error) {
	var public *apiError
	if errors.As(err, &public) {
		writeJSON(w, public.Status, public)
		return
	}
	requestID := randomToken(12)
	// Database and YAML errors can carry sensitive input; store only the ID and error type.
	log.Printf("studio request %s failed (%T)", requestID, err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{
		"code": "internal_error", "message": "暂时无法处理请求，请稍后再试。", "requestId": requestID,
	})
}

func readPositive(raw, label string) (int64, error) {
	num, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || num <= 0 {
		return 0, badRequest(label + "需要为正整数。")
	}
	return num, nil
}

func sourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type rateBucket struct {
	count     int
	expiresAt time.Time
}

type rateLimits struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
}

func (l *rateLimits) allow(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = make(map[string]rateBucket)
	}
	now := time.Now()
	bucket := l.buckets[key]
	if !now.Before(bucket.expiresAt) {
		if len(l.buckets) >= 4096 {
			for candidate, entry := range l.buckets {
				if !now.Before(entry.expiresAt) {
					delete(l.buckets, candidate)
				}
			}
			if len(l.buckets) >= 4096 {
				return false
			}
		}
		bucket = rateBucket{expiresAt: now.Add(window)}
	}
	if bucket.count >= max {
		return false
	}
	bucket.count++
	l.buckets[key] = bucket
	return true
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", s.api(s.register))
	mux.HandleFunc("POST /api/v1/auth/login", s.api(s.login))
	mux.HandleFunc("GET /api/v1/auth/me", s.protected(false, s.me))
	mux.HandleFunc("POST /api/v1/auth/logout", s.protected(true, s.logout))

	mux.HandleFunc("GET /api/v1/profiles", s.protected(false, s.listProfiles))
	mux.HandleFunc("POST /api/v1/profiles", s.protected(true, s.createProfile))
	mux.HandleFunc("GET /api/v1/profiles/{profile}", s.protected(false, s.getProfile))
	mux.HandleFunc("PATCH /api/v1/profiles/{profile}", s.protected(true, s.updateProfile))
	mux.HandleFunc("DELETE /api/v1/profiles/{profile}", s.protected(true, s.deleteProfile))
	mux.HandleFunc("POST /api/v1/profiles/{profile}/duplicate", s.protected(true, s.duplicateProfile))
	mux.HandleFunc("GET /api/v1/profiles/{profile}/draft", s.protected(false, s.getDraft))
	mux.HandleFunc("PATCH /api/v1/profiles/{profile}/draft", s.protected(true, s.patchDraft))
	mux.HandleFunc("GET /api/v1/profiles/{profile}/preview", s.protected(false, s.preview))
	mux.HandleFunc("GET /api/v1/profiles/{profile}/export.yaml", s.protected(false, s.export))
	mux.HandleFunc("POST /api/v1/profiles/{profile}/publish", s.protected(true, s.publish))
	mux.HandleFunc("GET /api/v1/profiles/{profile}/versions", s.protected(false, s.listVersions))
	mux.HandleFunc("POST /api/v1/profiles/{profile}/versions/{version}/restore", s.protected(true, s.restoreVersion))
	mux.HandleFunc("GET /api/v1/profiles/{profile}/links", s.protected(false, s.listLinks))
	mux.HandleFunc("POST /api/v1/profiles/{profile}/links", s.protected(true, s.createLink))
	mux.HandleFunc("GET /api/v1/profiles/{profile}/links/{link}/url", s.protected(false, s.getLinkURL))
	mux.HandleFunc("DELETE /api/v1/profiles/{profile}/links/{link}", s.protected(true, s.revokeLink))

	mux.HandleFunc("POST /api/v1/imports/probe", s.protected(true, s.probeImport))
	mux.HandleFunc("POST /api/v1/imports/provider-nodes", s.protected(true, s.providerNodes))
	mux.HandleFunc("POST /api/v1/profiles/{profile}/imports/refresh", s.protected(true, s.refreshImport))
	mux.HandleFunc("/api/v1/", s.api(func(http.ResponseWriter, *http.Request) error { return notFound() }))
	mux.HandleFunc("GET /api/v1/profiles/{profile}/subscription-key", s.protected(false, s.getSubscriptionKey))
	mux.HandleFunc("POST /api/v1/profiles/{profile}/subscription-key/rotate", s.protected(true, s.rotateSubscriptionKey))
	mux.HandleFunc("GET /s/{filename}", s.serveSubscription)
	return mux
}
