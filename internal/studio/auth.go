package studio

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	sessionCookie = "studio_session"
	sessionAge    = 7 * 24 * time.Hour
	passwordSalt  = 16
	passwordHash  = 32
)

type sessionUser struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	CSRFToken string `json:"csrfToken"`
	token     string
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func normalizedUsername(name string) (string, string) {
	trimmed := strings.TrimSpace(name)
	return trimmed, strings.ToLower(trimmed)
}

func acceptableUsername(name string) bool {
	if n := utf8.RuneCountInString(name); n < 2 || n > 40 {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_.-", r) {
			return false
		}
	}
	return true
}

func passwordKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 32*1024, 1, passwordHash)
}

func (s *Server) hashPassword(ctx context.Context, password string) ([]byte, error) {
	if err := s.acquirePasswordWork(ctx); err != nil {
		return nil, err
	}
	defer s.releasePasswordWork()

	stored := make([]byte, 1+passwordSalt, 1+passwordSalt+passwordHash)
	stored[0] = 1
	if _, err := rand.Read(stored[1:]); err != nil {
		return nil, err
	}
	return append(stored, passwordKey(password, stored[1:])...), nil
}

func (s *Server) matchPassword(ctx context.Context, stored []byte, password string) (bool, error) {
	if err := s.acquirePasswordWork(ctx); err != nil {
		return false, err
	}
	defer s.releasePasswordWork()

	valid := len(stored) == 1+passwordSalt+passwordHash && stored[0] == 1
	if !valid {
		// Match the cost of a valid account even when the name does not exist.
		stored = make([]byte, 1+passwordSalt+passwordHash)
	}
	actual := passwordKey(password, stored[1:1+passwordSalt])
	return subtle.ConstantTimeCompare(actual, stored[1+passwordSalt:]) == 1 && valid, nil
}

func (s *Server) acquirePasswordWork(ctx context.Context) error {
	select {
	case s.passwordWork <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return throttled()
	}
}

func (s *Server) releasePasswordWork() { <-s.passwordWork }

func (s *Server) requireCredentials(w http.ResponseWriter, r *http.Request, action string) (credentials, error) {
	if !s.sameOrigin(r) {
		return credentials{}, sameOriginRequired()
	}
	if !s.limits.allow("auth:"+sourceIP(r), 120, time.Minute) {
		w.Header().Set("Retry-After", "60")
		return credentials{}, throttled()
	}
	var input credentials
	if err := parseBody(w, r, &input); err != nil {
		return credentials{}, err
	}
	input.Username, _ = normalizedUsername(input.Username)
	if !acceptableUsername(input.Username) || len(input.Password) > 256 {
		if action == "register" {
			return credentials{}, badRequest("用户名使用 2–40 个字母、数字、下划线、点或连字符。密码最多 256 字节。")
		}
		return credentials{}, wrongCredentials()
	}
	_, normalized := normalizedUsername(input.Username)
	if !s.limits.allow(action+":"+sourceIP(r)+":"+normalized, 12, time.Minute) {
		w.Header().Set("Retry-After", "60")
		return credentials{}, throttled()
	}
	return input, nil
}

func wrongCredentials() error {
	return &apiError{Status: http.StatusUnauthorized, Code: "invalid_credentials", Message: "用户名或密码不正确。"}
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) error {
	input, err := s.requireCredentials(w, r, "register")
	if err != nil {
		return err
	}
	if utf8.RuneCountInString(input.Password) < 8 {
		return badRequest("密码需要至少 8 个字符。")
	}
	ctx := r.Context()
	hashed, err := s.hashPassword(ctx, input.Password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	user := sessionUser{ID: randomToken(18), Username: input.Username}
	_, normalized := normalizedUsername(input.Username)
	if _, err := tx.ExecContext(ctx, "INSERT INTO users VALUES(?, ?, ?, ?, ?)",
		user.ID, user.Username, normalized, hashed, time.Now().Unix()); err != nil {
		if sqliteUnique(err) {
			return &apiError{Status: http.StatusConflict, Code: "username_taken", Message: "用户名已被使用。"}
		}
		return err
	}
	if err := s.issueSession(ctx, tx, r, &user); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.sendSession(w, r, user, http.StatusCreated)
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	input, err := s.requireCredentials(w, r, "login")
	if err != nil {
		return err
	}
	ctx := r.Context()
	var user sessionUser
	var stored []byte
	_, normalized := normalizedUsername(input.Username)
	err = s.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash FROM users WHERE username_normalized = ?", normalized,
	).Scan(&user.ID, &user.Username, &stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	matches, err := s.matchPassword(ctx, stored, input.Password)
	if err != nil {
		return err
	}
	if !matches {
		return wrongCredentials()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.issueSession(ctx, tx, r, &user); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.sendSession(w, r, user, http.StatusOK)
	return nil
}

func (s *Server) issueSession(ctx context.Context, tx *sql.Tx, r *http.Request, user *sessionUser) error {
	now := time.Now()
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now.Unix()); err != nil {
		return err
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil && validToken(cookie.Value) {
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE session_digest = ?", digest(cookie.Value)); err != nil {
			return err
		}
	}
	user.token = randomToken(32)
	user.CSRFToken = s.sessionCSRF(user.token)
	_, err := tx.ExecContext(ctx, "INSERT INTO sessions VALUES(?, ?, ?, ?)",
		digest(user.token), user.ID, now.Add(sessionAge).Unix(), now.Unix())
	return err
}

func validToken(token string) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(32) {
		return false
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(data) == 32
}

func (s *Server) sessionCSRF(token string) string {
	h := hmac.New(sha256.New, s.csrfKey)
	h.Write([]byte(token))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (s *Server) sendSession(w http.ResponseWriter, r *http.Request, user sessionUser, status int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: user.token, Path: "/", HttpOnly: true, Secure: s.useSecureCookie(r),
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionAge / time.Second),
	})
	writeJSON(w, status, user)
}

func (s *Server) authenticate(r *http.Request) (sessionUser, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || !validToken(cookie.Value) {
		return sessionUser{}, unauthenticated()
	}
	user := sessionUser{token: cookie.Value, CSRFToken: s.sessionCSRF(cookie.Value)}
	err = s.db.QueryRowContext(r.Context(), `
		SELECT u.id, u.username
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.session_digest = ? AND s.expires_at > ?`, digest(user.token), time.Now().Unix(),
	).Scan(&user.ID, &user.Username)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return sessionUser{}, unauthenticated()
	case err != nil:
		return sessionUser{}, err
	default:
		return user, nil
	}
}

func (s *Server) protected(write bool, fn userHandler) http.HandlerFunc {
	return s.api(func(w http.ResponseWriter, r *http.Request) error {
		user, err := s.authenticate(r)
		if err != nil {
			return err
		}
		if write {
			if !s.sameOrigin(r) {
				return sameOriginRequired()
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(user.CSRFToken)) != 1 {
				return &apiError{Status: http.StatusForbidden, Code: "invalid_csrf", Message: "会话验证失效，请刷新页面。"}
			}
		}
		return fn(w, r, user)
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	writeJSON(w, http.StatusOK, user)
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	if _, err := s.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE session_digest = ?", digest(user.token)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Path: "/", HttpOnly: true, Secure: s.useSecureCookie(r), SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(1, 0).UTC(),
	})
	w.WriteHeader(http.StatusNoContent)
	return nil
}
