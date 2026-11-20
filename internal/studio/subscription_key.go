package studio

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type subscriptionKey struct {
	Key       string    `json:"key"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func subscriptionKeyPurpose(id string) string { return "profile:" + id + ":subscription-key" }

func (s *Server) replaceSubscriptionKey(ctx context.Context, q executor, profileID string) (subscriptionKey, error) {
	key := subscriptionKey{Key: randomToken(32), UpdatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	_, err := q.ExecContext(ctx, `
		INSERT INTO profile_subscription_keys(profile_id, key_digest, sealed_key, updated_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(profile_id) DO UPDATE SET
			key_digest = excluded.key_digest, sealed_key = excluded.sealed_key, updated_at = excluded.updated_at`,
		profileID, digest(key.Key), s.seal(key.Key, subscriptionKeyPurpose(profileID)), key.UpdatedAt.UnixMilli())
	return key, err
}

func (s *Server) getSubscriptionKey(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	id := r.PathValue("profile")
	var key subscriptionKey
	var sealed []byte
	var updated int64
	err := s.db.QueryRowContext(r.Context(), `
		SELECT k.sealed_key, k.updated_at FROM profile_subscription_keys k
		JOIN profiles p ON p.id = k.profile_id WHERE p.id = ? AND p.owner_id = ?`, id, user.ID).Scan(&sealed, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound()
	}
	if err != nil {
		return err
	}
	key.Key, err = s.unseal(sealed, subscriptionKeyPurpose(id))
	if err != nil {
		return err
	}
	key.UpdatedAt = time.UnixMilli(updated).UTC()
	writeJSON(w, http.StatusOK, key)
	return nil
}

func (s *Server) rotateSubscriptionKey(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := r.PathValue("profile")
	if _, err := readProfile(ctx, tx, user.ID, id); err != nil {
		return err
	}
	key, err := s.replaceSubscriptionKey(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, key)
	return nil
}

func requestSubscriptionKey(r *http.Request) string {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return ""
	}
	queryKeys := values["key"]
	headers := r.Header.Values("Authorization")
	switch {
	case len(queryKeys) == 1 && len(headers) == 0:
		return queryKeys[0]
	case len(queryKeys) == 0 && len(headers) == 1:
		scheme, key, ok := strings.Cut(headers[0], " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			return key
		}
	}
	return ""
}
