package studio

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Version struct {
	Version   int64     `json:"version"`
	DraftRev  int64     `json:"draftRev"`
	CreatedAt time.Time `json:"createdAt"`
	yaml      string
}

type Link struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

func scanVersion(row scanner) (Version, error) {
	var version Version
	var created int64
	err := row.Scan(&version.Version, &version.DraftRev, &created)
	version.CreatedAt = time.UnixMilli(created).UTC()
	return version, err
}

func findPublishedDraft(ctx context.Context, q queryable, owner, id string, rev int64) (Version, error) {
	return scanVersion(q.QueryRowContext(ctx, `
		SELECT v.version, v.draft_rev, v.created_at
		FROM profile_versions v JOIN profiles p ON p.id = v.profile_id
		WHERE p.owner_id = ? AND p.id = ? AND v.draft_rev = ?`, owner, id, rev))
}

func (s *Server) readVersion(ctx context.Context, q queryable, owner, profileID string, number int64) (Version, error) {
	var version Version
	var id string
	var created int64
	var sealed []byte
	err := q.QueryRowContext(ctx, `
		SELECT v.id, v.version, v.draft_rev, v.created_at, v.yaml
		FROM profile_versions v JOIN profiles p ON p.id = v.profile_id
		WHERE p.owner_id = ? AND p.id = ? AND v.version = ?`, owner, profileID, number,
	).Scan(&id, &version.Version, &version.DraftRev, &created, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return version, notFound()
	}
	if err != nil {
		return version, err
	}
	version.CreatedAt = time.UnixMilli(created).UTC()
	version.yaml, err = s.unseal(sealed, versionPurpose(id))
	return version, err
}

func versionPurpose(id string) string { return "version:" + id + ":yaml" }

func (s *Server) publish(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	var req revisionRequest
	if err := parseBody(w, r, &req); err != nil {
		return err
	}
	if req.ExpectedDraftRev <= 0 {
		return badRequest("请指定要发布的草稿修订。")
	}
	ctx := r.Context()
	id := r.PathValue("profile")
	version, err := findPublishedDraft(ctx, s.db, user.ID, id, req.ExpectedDraftRev)
	if err == nil {
		writeJSON(w, http.StatusOK, version)
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	draft, err := s.readDraft(ctx, s.db, user.ID, id)
	if err != nil {
		return err
	}
	if draft.DraftRev != req.ExpectedDraftRev {
		return conflict()
	}
	if err := validateOutput(draft.YAML); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// An equal revision may have published while the YAML was being checked.
	version, err = findPublishedDraft(ctx, tx, user.ID, id, draft.DraftRev)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, version)
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	profile, err := readProfile(ctx, tx, user.ID, id)
	if err != nil {
		return err
	}
	if profile.DraftRev != draft.DraftRev {
		return conflict()
	}
	version = Version{DraftRev: draft.DraftRev, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version),0)+1 FROM profile_versions WHERE profile_id = ?", id,
	).Scan(&version.Version); err != nil {
		return err
	}
	versionID := randomToken(18)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO profile_versions(id, profile_id, version, draft_rev, yaml, content_sha256, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, versionID, id, version.Version, version.DraftRev,
		s.seal(draft.YAML, versionPurpose(versionID)), digest(draft.YAML), version.CreatedAt.UnixMilli()); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE profiles SET published_version_id = ?, updated_at = ? WHERE id = ? AND owner_id = ? AND draft_rev = ?`,
		versionID, version.CreatedAt.UnixMilli(), id, user.ID, draft.DraftRev)
	if err := requireAffected(result, err, conflict); err != nil {
		return err
	}
	if version.Version == 1 {
		if _, err := s.insertLink(ctx, tx, id, "默认订阅"); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, version)
	return nil
}

func (s *Server) listVersions(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	ctx := r.Context()
	id := r.PathValue("profile")
	if _, err := readProfile(ctx, s.db, user.ID, id); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.version, v.draft_rev, v.created_at
		FROM profile_versions v JOIN profiles p ON p.id = v.profile_id
		WHERE p.owner_id = ? AND p.id = ? ORDER BY v.version DESC`, user.ID, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	versions := make([]Version, 0)
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string][]Version{"versions": versions})
	return nil
}

func (s *Server) restoreVersion(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	number, err := readPositive(r.PathValue("version"), "发布版本")
	if err != nil {
		return err
	}
	var req revisionRequest
	if err := parseBody(w, r, &req); err != nil {
		return err
	}
	if req.ExpectedDraftRev <= 0 {
		return badRequest("请指定要覆盖的草稿修订。")
	}
	ctx := r.Context()
	id := r.PathValue("profile")
	version, err := s.readVersion(ctx, s.db, user.ID, id, number)
	if err != nil {
		return err
	}
	draft, err := s.saveDraft(ctx, s.db, user.ID, id, req.ExpectedDraftRev, version.yaml, true)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, draft)
	return nil
}

func (s *Server) insertLink(ctx context.Context, tx *sql.Tx, profile, label string) (Link, error) {
	link := Link{ID: randomToken(18), Label: label, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO subscription_links(id, profile_id, label, created_at)
		VALUES(?, ?, ?, ?)`, link.ID, profile, link.Label, link.CreatedAt.UnixMilli())
	return link, err
}

func (s *Server) listLinks(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	ctx := r.Context()
	profileID := r.PathValue("profile")
	if _, err := readProfile(ctx, s.db, user.ID, profileID); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.label, l.created_at, l.revoked_at FROM subscription_links l
		JOIN profiles p ON p.id = l.profile_id
		WHERE p.owner_id = ? AND p.id = ? ORDER BY l.created_at, l.id`, user.ID, profileID)
	if err != nil {
		return err
	}
	defer rows.Close()
	links := make([]Link, 0)
	for rows.Next() {
		var link Link
		var created int64
		var revoked sql.NullInt64
		if err := rows.Scan(&link.ID, &link.Label, &created, &revoked); err != nil {
			return err
		}
		link.CreatedAt = time.UnixMilli(created).UTC()
		if revoked.Valid {
			at := time.UnixMilli(revoked.Int64).UTC()
			link.RevokedAt = &at
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string][]Link{"links": links})
	return nil
}

func (s *Server) createLink(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	var req struct {
		Label string `json:"label"`
	}
	if err := parseBody(w, r, &req); err != nil {
		return err
	}
	label, ok := validateDisplayName(req.Label, 60)
	if !ok {
		return badRequest("链接名称需要 1–60 个字符。")
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	profile, err := readProfile(ctx, tx, user.ID, r.PathValue("profile"))
	if err != nil {
		return err
	}
	if profile.PublishedVersion <= 0 {
		return &apiError{Status: http.StatusConflict, Code: "not_published", Message: "请先发布配置。"}
	}
	link, err := s.insertLink(ctx, tx, profile.ID, label)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, link)
	return nil
}

func (s *Server) getLinkURL(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	id, profileID := r.PathValue("link"), r.PathValue("profile")
	var sealed []byte
	err := s.db.QueryRowContext(r.Context(), `
		SELECT k.sealed_key FROM subscription_links l JOIN profiles p ON p.id = l.profile_id
		JOIN profile_subscription_keys k ON k.profile_id = p.id
		WHERE p.owner_id = ? AND p.id = ? AND l.id = ? AND l.revoked_at IS NULL AND p.published_version_id IS NOT NULL`,
		user.ID, profileID, id,
	).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound()
	}
	if err != nil {
		return err
	}
	key, err := s.unseal(sealed, subscriptionKeyPurpose(profileID))
	if err != nil {
		return err
	}
	base := s.publicBase
	if base == "" {
		base = requestBase(r)
	}
	endpoint := base + "/s/" + id + ".yaml"
	writeJSON(w, http.StatusOK, map[string]string{"endpoint": endpoint, "url": endpoint + "?key=" + url.QueryEscape(key)})
	return nil
}

func (s *Server) revokeLink(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	result, err := s.db.ExecContext(r.Context(), `
		UPDATE subscription_links SET revoked_at = COALESCE(revoked_at, ?)
		WHERE id = ? AND profile_id = (SELECT id FROM profiles WHERE id = ? AND owner_id = ?)`,
		time.Now().UnixMilli(), r.PathValue("link"), r.PathValue("profile"), user.ID)
	if err := requireAffected(result, err, notFound); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) serveSubscription(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !s.limits.allow("subscription:"+sourceIP(r), 240, time.Minute) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	id, suffix := strings.CutSuffix(r.PathValue("filename"), ".yaml")
	key := requestSubscriptionKey(r)
	if !suffix || len(id) != 24 || !validToken(key) {
		http.NotFound(w, r)
		return
	}
	var versionID string
	var hash, sealed []byte
	err := s.db.QueryRowContext(r.Context(), `
		SELECT v.id, v.content_sha256, v.yaml FROM subscription_links l
		JOIN profiles p ON p.id = l.profile_id
		JOIN profile_versions v ON v.id = p.published_version_id AND v.profile_id = p.id
		JOIN profile_subscription_keys k ON k.profile_id = p.id
		WHERE l.id = ? AND k.key_digest = ? AND l.revoked_at IS NULL`, id, digest(key),
	).Scan(&versionID, &hash, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeAPIError(w, err)
		return
	}
	tag := `"` + hex.EncodeToString(hash) + `"`
	w.Header().Set("ETag", tag)
	if matchesETag(r.Header.Values("If-None-Match"), tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	yaml, err := s.unseal(sealed, versionPurpose(versionID))
	if err != nil {
		w.Header().Del("ETag")
		writeAPIError(w, err)
		return
	}
	writeYAML(w, r, yaml)
}

func matchesETag(headers []string, tag string) bool {
	for _, header := range headers {
		for _, token := range strings.Split(header, ",") {
			token = strings.TrimSpace(token)
			if token == "*" || strings.TrimPrefix(token, "W/") == tag {
				return true
			}
		}
	}
	return false
}
