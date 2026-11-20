package studio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"clash-rule-gen/internal/config"
)

const initialYAML = "mixed-port: 7890\nmode: rule\nrules:\n  - MATCH,DIRECT\n"

type Profile struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Target           string    `json:"target"`
	DraftRev         int64     `json:"draftRev"`
	PublishedVersion int64     `json:"publishedVersion,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt"`
	HasSource        bool      `json:"hasSource"`
}

type Draft struct {
	DraftRev int64  `json:"draftRev"`
	YAML     string `json:"yaml"`
}

type revisionRequest struct {
	ExpectedDraftRev int64 `json:"expectedDraftRev"`
}

type profileMetadata struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

type draftMutation struct {
	revisionRequest
	Changes map[string]json.RawMessage `json:"changes"`
	Remove  []string                   `json:"remove,omitempty"`
	Rename  *config.Rename             `json:"rename,omitempty"`
}

type createProfileRequest struct {
	profileMetadata
	YAML     *string `json:"yaml,omitempty"`
	ImportID string  `json:"importId,omitempty"`
}

type queryable interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type scanner interface{ Scan(...any) error }

func validateDisplayName(value string, max int) (string, bool) {
	value = strings.TrimSpace(value)
	if n := utf8.RuneCountInString(value); n == 0 || n > max {
		return "", false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return value, true
}

func validateProfileMetadata(meta profileMetadata) (profileMetadata, error) {
	var ok bool
	meta.Name, ok = validateDisplayName(meta.Name, 80)
	if !ok {
		return meta, badRequest("配置名称需要 1–80 个字符。")
	}
	switch meta.Target {
	case "openclash", "verge", "both":
		return meta, nil
	default:
		return meta, badRequest("目标客户端无效。")
	}
}

const profileSelect = `
	SELECT p.id, p.name, p.target, p.draft_rev, COALESCE(v.version,0), p.updated_at,
	       EXISTS (SELECT 1 FROM profile_imports WHERE profile_id = p.id)
	FROM profiles p
	LEFT JOIN profile_versions v ON v.id = p.published_version_id AND v.profile_id = p.id`

func scanProfile(row scanner) (Profile, error) {
	var profile Profile
	var updated int64
	err := row.Scan(&profile.ID, &profile.Name, &profile.Target, &profile.DraftRev, &profile.PublishedVersion, &updated, &profile.HasSource)
	if errors.Is(err, sql.ErrNoRows) {
		return profile, notFound()
	}
	profile.UpdatedAt = time.UnixMilli(updated).UTC()
	return profile, err
}

func readProfile(ctx context.Context, q queryable, owner, id string) (Profile, error) {
	return scanProfile(q.QueryRowContext(ctx, profileSelect+" WHERE p.owner_id = ? AND p.id = ?", owner, id))
}

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	params := r.URL.Query()
	const pageSize = 100
	limit := int64(pageSize)
	offset := int64(0)
	if raw := params.Get("limit"); raw != "" {
		num, err := readPositive(raw, "每页条数")
		if err != nil || num > pageSize {
			return badRequest("每页条数范围为 1–100。")
		}
		limit = num
	}
	if raw := params.Get("offset"); raw != "" {
		num, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || num < 0 || num > 1_000_000 {
			return badRequest("分页起始位置无效。")
		}
		offset = num
	}
	rows, err := s.db.QueryContext(r.Context(), profileSelect+`
		WHERE p.owner_id = ? ORDER BY p.updated_at DESC, p.id LIMIT ? OFFSET ?`, user.ID, limit+1, offset)
	if err != nil {
		return err
	}
	defer rows.Close()
	profiles := make([]Profile, 0)
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err != nil {
			return err
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	result := struct {
		Profiles   []Profile `json:"profiles"`
		NextOffset int64     `json:"nextOffset,omitempty"`
	}{Profiles: profiles}
	if int64(len(profiles)) > limit {
		result.Profiles = profiles[:limit]
		result.NextOffset = offset + limit
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	profile, err := readProfile(r.Context(), s.db, user.ID, r.PathValue("profile"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, profile)
	return nil
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	var req createProfileRequest
	if err := parseBody(w, r, &req); err != nil {
		return err
	}
	meta, err := validateProfileMetadata(req.profileMetadata)
	if err != nil {
		return err
	}
	if req.YAML != nil && req.ImportID != "" {
		return badRequest("请选择 YAML 或已探测的导入记录。")
	}
	yaml := initialYAML
	if req.YAML != nil {
		doc, err := config.ParseImport(*req.YAML, "config")
		if err != nil {
			return invalidConfig(err.Error(), nil)
		}
		yaml, err = doc.YAML()
		if err != nil {
			return err
		}
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var source sourceConfig
	if req.ImportID != "" {
		stage, err := s.readImportStage(ctx, tx, req.ImportID, user.ID, "")
		if err != nil {
			return err
		}
		yaml, source = stage.YAML, stage.Source
	}
	profile, err := s.insertProfile(ctx, tx, user.ID, meta, yaml)
	if err != nil {
		return err
	}
	if source.URL != "" {
		if err := s.recordSource(ctx, tx, profile.ID, source, yaml); err != nil {
			return err
		}
		profile.HasSource = true
	}
	if req.ImportID != "" {
		if _, err := tx.ExecContext(ctx, "DELETE FROM import_stages WHERE id = ?", req.ImportID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, profile)
	return nil
}

func (s *Server) insertProfile(ctx context.Context, tx *sql.Tx, owner string, meta profileMetadata, yaml string) (Profile, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	profile := Profile{
		ID: randomToken(18), Name: meta.Name, Target: meta.Target, DraftRev: 1, UpdatedAt: now,
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO profiles(id, owner_id, name, target, draft_yaml, draft_rev, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, profile.ID, owner, meta.Name, meta.Target,
		s.seal(yaml, draftPurpose(profile.ID)), profile.DraftRev, now.UnixMilli())
	return profile, err
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	var input profileMetadata
	if err := parseBody(w, r, &input); err != nil {
		return err
	}
	meta, err := validateProfileMetadata(input)
	if err != nil {
		return err
	}
	ctx := r.Context()
	id := r.PathValue("profile")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE profiles SET name = ?, target = ?, updated_at = ?
		WHERE owner_id = ? AND id = ?`, meta.Name, meta.Target, time.Now().UnixMilli(), user.ID, id)
	if err := requireAffected(result, err, notFound); err != nil {
		return err
	}
	profile, err := readProfile(ctx, tx, user.ID, id)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, profile)
	return nil
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	ctx := r.Context()
	id := r.PathValue("profile")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE profiles SET published_version_id = NULL
		WHERE id = ? AND owner_id = ?`, id, user.ID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM profiles WHERE id = ? AND owner_id = ?", id, user.ID)
	if err := requireAffected(result, err, notFound); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) duplicateProfile(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	ctx := r.Context()
	id := r.PathValue("profile")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	source, err := readProfile(ctx, tx, user.ID, id)
	if err != nil {
		return err
	}
	draft, err := s.readDraft(ctx, tx, user.ID, id)
	if err != nil {
		return err
	}
	name := []rune(source.Name)
	const suffix = "（副本）"
	if max := 80 - utf8.RuneCountInString(suffix); len(name) > max {
		name = name[:max]
	}
	profile, err := s.insertProfile(ctx, tx, user.ID, profileMetadata{Name: string(name) + suffix, Target: source.Target}, draft.YAML)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, profile)
	return nil
}

func (s *Server) readDraft(ctx context.Context, q queryable, owner, id string) (Draft, error) {
	var draft Draft
	var sealed []byte
	err := q.QueryRowContext(ctx,
		"SELECT draft_rev, draft_yaml FROM profiles WHERE owner_id = ? AND id = ?", owner, id,
	).Scan(&draft.DraftRev, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return draft, notFound()
	}
	if err != nil {
		return draft, err
	}
	draft.YAML, err = s.unseal(sealed, draftPurpose(id))
	return draft, err
}

func draftPurpose(profileID string) string { return "profile:" + profileID + ":draft" }

func (s *Server) getDraft(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	draft, err := s.readDraft(r.Context(), s.db, user.ID, r.PathValue("profile"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, draft)
	return nil
}

func (s *Server) patchDraft(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	var req draftMutation
	if err := parseBody(w, r, &req); err != nil {
		return err
	}
	if req.ExpectedDraftRev <= 0 {
		return badRequest("请指定要修改的草稿修订。")
	}
	ctx := r.Context()
	id := r.PathValue("profile")
	original, err := s.readDraft(ctx, s.db, user.ID, id)
	if err != nil {
		return err
	}
	if original.DraftRev != req.ExpectedDraftRev {
		return conflict()
	}
	doc, err := config.Parse(original.YAML)
	if err != nil {
		return err
	}
	if err := doc.Apply(req.Changes, req.Remove, req.Rename); err != nil {
		return invalidConfig(err.Error(), nil)
	}
	yaml, err := doc.YAML()
	if err != nil {
		return invalidConfig(err.Error(), nil)
	}
	// The time-consuming YAML work runs before the SQLite write; the revision is
	// checked again by the update to avoid overwriting edits made meanwhile.
	draft, err := s.saveDraft(ctx, s.db, user.ID, id, original.DraftRev, yaml, yaml != original.YAML)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, draft)
	return nil
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Server) saveDraft(ctx context.Context, q executor, owner, id string, expected int64, yaml string, changed bool) (Draft, error) {
	draft := Draft{DraftRev: expected, YAML: yaml}
	var result sql.Result
	var err error
	if changed {
		draft.DraftRev++
		result, err = q.ExecContext(ctx, `UPDATE profiles SET draft_yaml = ?, draft_rev = ?, updated_at = ?
			WHERE id = ? AND owner_id = ? AND draft_rev = ?`,
			s.seal(yaml, draftPurpose(id)), draft.DraftRev, time.Now().UnixMilli(), id, owner, expected)
	} else {
		result, err = q.ExecContext(ctx, `UPDATE profiles SET draft_rev = draft_rev WHERE id = ? AND owner_id = ? AND draft_rev = ?`, id, owner, expected)
	}
	return draft, requireAffected(result, err, conflict)
}

func requireAffected(result sql.Result, err error, missing func() error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return missing()
	}
	return nil
}

func readDraftRevision(r *http.Request, draft Draft) error {
	if raw := r.URL.Query().Get("rev"); raw != "" {
		wanted, err := readPositive(raw, "草稿修订")
		if err != nil {
			return err
		}
		if draft.DraftRev != wanted {
			return conflict()
		}
	}
	return nil
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	ctx := r.Context()
	id := r.PathValue("profile")
	params := r.URL.Query()
	var output Draft
	if raw := params.Get("version"); raw != "" {
		if params.Get("rev") != "" {
			return badRequest("请选择草稿或一个发布版本。")
		}
		number, err := readPositive(raw, "发布版本")
		if err != nil {
			return err
		}
		version, err := s.readVersion(ctx, s.db, user.ID, id, number)
		if err != nil {
			return err
		}
		output = Draft{DraftRev: version.DraftRev, YAML: version.yaml}
	} else {
		var err error
		output, err = s.readDraft(ctx, s.db, user.ID, id)
		if err != nil {
			return err
		}
		if err := readDraftRevision(r, output); err != nil {
			return err
		}
	}
	doc, err := config.Parse(output.YAML)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, struct {
		Draft
		Issues []config.Issue `json:"issues"`
	}{Draft: output, Issues: issuesFor(doc)})
	return nil
}

func issuesFor(doc *config.Document) []config.Issue {
	issues := doc.Validate()
	if issues == nil {
		return []config.Issue{}
	}
	return issues
}

func validateOutput(yaml string) error {
	doc, err := config.Parse(yaml)
	if err != nil {
		return invalidConfig(err.Error(), nil)
	}
	issues := doc.Validate()
	for _, item := range issues {
		if item.Severity == "error" {
			return invalidConfig("请先修复配置中的错误。", issues)
		}
	}
	return nil
}

func (s *Server) export(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	ctx := r.Context()
	id := r.PathValue("profile")
	draft, err := s.readDraft(ctx, s.db, user.ID, id)
	if err != nil {
		return err
	}
	if err := readDraftRevision(r, draft); err != nil {
		return err
	}
	if err := validateOutput(draft.YAML); err != nil {
		return err
	}
	profile, err := readProfile(ctx, s.db, user.ID, id)
	if err != nil {
		return err
	}
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '-'
		}
		return r
	}, profile.Name)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".yaml"}))
	writeYAML(w, r, draft.YAML)
	return nil
}

func writeYAML(w http.ResponseWriter, r *http.Request, yaml string) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(yaml)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, yaml)
	}
}
