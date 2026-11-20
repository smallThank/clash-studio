package studio

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"clash-rule-gen/internal/config"
)

const stageLifetime = 15 * time.Minute

type sourceConfig struct {
	URL       string
	UserAgent string
}

type importStage struct {
	ID       string
	YAML     string
	Source   sourceConfig
	DraftRev int64
	BaseHash []byte
}

type currentSource struct {
	Source   sourceConfig
	BaseYAML string
	BaseHash []byte
	Draft    Draft
}

func sourcePurpose(id, field string) string { return "profile:" + id + ":source:" + field }
func stagePurpose(id, field string) string  { return "stage:" + id + ":" + field }

type probeRequest struct {
	Mode      string  `json:"mode"`
	YAML      *string `json:"yaml,omitempty"`
	URL       string  `json:"url,omitempty"`
	UserAgent string  `json:"userAgent,omitempty"`
}

func subscriptionSource(rawURL, userAgent string) (sourceConfig, string, error) {
	if len(rawURL) > 4096 || len(userAgent) > 256 || strings.ContainsFunc(userAgent, unicode.IsControl) {
		return sourceConfig{}, "", badRequest("订阅地址或 User-Agent 无效。")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" ||
		parsed.Opaque != "" || parsed.User != nil || parsed.Fragment != "" {
		return sourceConfig{}, "", badRequest("请填写有效的 HTTP 或 HTTPS 订阅地址。")
	}
	return sourceConfig{URL: rawURL, UserAgent: userAgent}, parsed.Host, nil
}

type fetchFunc func(context.Context, string, string) (string, error)

func importFetcher(injected fetchFunc) fetchFunc {
	network := (&config.Fetcher{}).Fetch
	fetch := network
	if injected != nil {
		fetch = injected
	}
	return func(ctx context.Context, rawURL, userAgent string) (string, error) {
		yaml, err := fetch(ctx, rawURL, userAgent)
		if err == nil {
			return yaml, nil
		}
		message := "无法读取原始订阅。请检查地址、访问权限以及服务器能否访问该来源。"
		if injected == nil {
			// The built-in fetcher emits only sanitized classifications and HTTP status codes.
			message = err.Error()
		}
		return "", &apiError{Status: http.StatusUnprocessableEntity, Code: "source_unavailable", Message: message}
	}
}

func (s *Server) probeImport(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	var req probeRequest
	if err := parseBody(w, r, &req); err != nil {
		return err
	}
	if req.Mode != "config" && req.Mode != "provider" {
		return badRequest("请选择导入完整配置或探测节点订阅。")
	}
	rawURL := strings.TrimSpace(req.URL)
	if (req.YAML == nil) == (rawURL == "") {
		return badRequest("请提供 YAML 内容或原始订阅地址。")
	}
	if !s.limits.allow("import:"+user.ID, 24, 5*time.Minute) {
		return throttled()
	}
	ctx := r.Context()
	var source sourceConfig
	var rawYAML, host string
	if req.YAML != nil {
		rawYAML = *req.YAML
	} else {
		var err error
		source, host, err = subscriptionSource(rawURL, strings.TrimSpace(req.UserAgent))
		if err != nil {
			return err
		}
		rawYAML, err = s.fetch(ctx, source.URL, source.UserAgent)
		if err != nil {
			return err
		}
	}
	doc, err := config.ParseImport(rawYAML, req.Mode)
	if err != nil {
		return invalidConfig(err.Error(), nil)
	}
	response := struct {
		ID     string         `json:"id,omitempty"`
		Kind   string         `json:"kind"`
		YAML   string         `json:"yaml,omitempty"`
		Host   string         `json:"host,omitempty"`
		Issues []config.Issue `json:"issues"`
	}{Kind: req.Mode, Host: host, Issues: []config.Issue{}}
	if req.Mode == "config" {
		response.Issues = issuesFor(doc)
		response.YAML, err = doc.YAML()
		if err != nil {
			return invalidConfig(err.Error(), nil)
		}
		response.ID, err = s.stageImport(ctx, user.ID, "", importStage{
			YAML: response.YAML, Source: source,
		})
		if err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, response)
	return nil
}

func (s *Server) stageImport(ctx context.Context, owner, profile string, values importStage) (string, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM import_stages WHERE expires_at <= ?", now.Unix()); err != nil {
		return "", err
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM import_stages WHERE owner_id = ?", owner).Scan(&count); err != nil {
		return "", err
	}
	if count >= 24 {
		return "", throttled()
	}
	id := randomToken(18)
	var sealedURL, sealedAgent []byte
	if values.Source.URL != "" {
		sealedURL = s.seal(values.Source.URL, stagePurpose(id, "url"))
		sealedAgent = s.seal(values.Source.UserAgent, stagePurpose(id, "agent"))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO import_stages(id, owner_id, profile_id, draft_rev, base_sha256, yaml, source_url, user_agent, expires_at)
		VALUES(?, ?, NULLIF(?,''), NULLIF(?,0), ?, ?, ?, ?, ?)`,
		id, owner, profile, values.DraftRev, values.BaseHash,
		s.seal(values.YAML, stagePurpose(id, "yaml")), sealedURL, sealedAgent, now.Add(stageLifetime).Unix()); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (s *Server) readImportStage(ctx context.Context, q queryable, id, owner, profile string) (importStage, error) {
	stage := importStage{ID: id}
	var sealedYAML, sealedURL, sealedAgent []byte
	err := q.QueryRowContext(ctx, `
		SELECT yaml, source_url, user_agent, COALESCE(draft_rev,0), base_sha256
		FROM import_stages
		WHERE id = ? AND owner_id = ? AND COALESCE(profile_id,'') = ? AND expires_at > ?`,
		id, owner, profile, time.Now().Unix(),
	).Scan(&sealedYAML, &sealedURL, &sealedAgent, &stage.DraftRev, &stage.BaseHash)
	if errors.Is(err, sql.ErrNoRows) {
		return stage, notFound()
	}
	if err != nil {
		return stage, err
	}
	stage.YAML, err = s.unseal(sealedYAML, stagePurpose(id, "yaml"))
	if err != nil || len(sealedURL) == 0 {
		return stage, err
	}
	stage.Source.URL, err = s.unseal(sealedURL, stagePurpose(id, "url"))
	if err != nil {
		return stage, err
	}
	stage.Source.UserAgent, err = s.unseal(sealedAgent, stagePurpose(id, "agent"))
	return stage, err
}

func (s *Server) recordSource(ctx context.Context, tx *sql.Tx, id string, source sourceConfig, yaml string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO profile_imports(profile_id, source_url, user_agent, base_yaml, content_sha256, last_fetched_at)
		VALUES(?, ?, ?, ?, ?, ?)`, id,
		s.seal(source.URL, sourcePurpose(id, "url")), s.seal(source.UserAgent, sourcePurpose(id, "agent")),
		s.seal(yaml, sourcePurpose(id, "base")), digest(yaml), time.Now().UnixMilli())
	return err
}

func (s *Server) readSource(ctx context.Context, owner, id string) (currentSource, error) {
	var state currentSource
	var sealedDraft, sealedBase, sealedURL, sealedAgent []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT p.draft_rev, p.draft_yaml, i.base_yaml, i.content_sha256, i.source_url, i.user_agent
		FROM profiles p JOIN profile_imports i ON i.profile_id = p.id WHERE p.id = ? AND p.owner_id = ?`,
		id, owner,
	).Scan(&state.Draft.DraftRev, &sealedDraft, &sealedBase, &state.BaseHash, &sealedURL, &sealedAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return state, notFound()
	}
	if err != nil {
		return state, err
	}
	fields := []struct {
		sealed  []byte
		dest    *string
		purpose string
	}{
		{sealedDraft, &state.Draft.YAML, draftPurpose(id)},
		{sealedBase, &state.BaseYAML, sourcePurpose(id, "base")},
		{sealedURL, &state.Source.URL, sourcePurpose(id, "url")},
		{sealedAgent, &state.Source.UserAgent, sourcePurpose(id, "agent")},
	}
	for _, field := range fields {
		if *field.dest, err = s.unseal(field.sealed, field.purpose); err != nil {
			return state, err
		}
	}
	return state, nil
}

type refreshRequest struct {
	revisionRequest
	Action      string            `json:"action"`
	ImportID    string            `json:"importId,omitempty"`
	Resolutions map[string]string `json:"resolutions,omitempty"`
}

func (s *Server) refreshImport(w http.ResponseWriter, r *http.Request, user sessionUser) error {
	var req refreshRequest
	if err := parseBody(w, r, &req); err != nil {
		return err
	}
	id := r.PathValue("profile")
	ctx := r.Context()
	switch req.Action {
	case "preview":
		if !s.limits.allow("import:"+user.ID, 24, 5*time.Minute) {
			return throttled()
		}
		return s.previewRefresh(w, ctx, user.ID, id)
	case "apply":
		return s.applyRefresh(w, ctx, user.ID, id, req)
	default:
		return badRequest("请选择预览或应用订阅更新。")
	}
}

func (s *Server) previewRefresh(w http.ResponseWriter, ctx context.Context, owner, id string) error {
	current, err := s.readSource(ctx, owner, id)
	if err != nil {
		return err
	}
	raw, err := s.fetch(ctx, current.Source.URL, current.Source.UserAgent)
	if err != nil {
		return err
	}
	doc, err := config.ParseImport(raw, "config")
	if err != nil {
		return invalidConfig(err.Error(), nil)
	}
	upstream, err := doc.YAML()
	if err != nil {
		return invalidConfig(err.Error(), nil)
	}
	yaml, conflicts, err := config.Merge(current.BaseYAML, current.Draft.YAML, upstream, nil)
	if err != nil {
		return invalidConfig(err.Error(), nil)
	}
	stage, err := s.stageImport(ctx, owner, id, importStage{
		YAML: upstream, DraftRev: current.Draft.DraftRev, BaseHash: current.BaseHash,
	})
	if err != nil {
		return err
	}
	if conflicts == nil {
		conflicts = []config.Conflict{}
	}
	writeJSON(w, http.StatusOK, struct {
		ID        string            `json:"id"`
		YAML      string            `json:"yaml"`
		Conflicts []config.Conflict `json:"conflicts"`
		DraftRev  int64             `json:"draftRev"`
	}{ID: stage, YAML: yaml, Conflicts: conflicts, DraftRev: current.Draft.DraftRev})
	return nil
}

func (s *Server) applyRefresh(w http.ResponseWriter, ctx context.Context, owner, id string, req refreshRequest) error {
	if req.ImportID == "" || req.ExpectedDraftRev <= 0 {
		return badRequest("请指定待应用的导入预览和草稿修订。")
	}
	for _, resolution := range req.Resolutions {
		if resolution != "draft" && resolution != "upstream" {
			return badRequest("冲突解决方案无效。")
		}
	}
	stage, err := s.readImportStage(ctx, s.db, req.ImportID, owner, id)
	if err != nil {
		return err
	}
	current, err := s.readSource(ctx, owner, id)
	if err != nil {
		return err
	}
	if current.Draft.DraftRev != req.ExpectedDraftRev || stage.DraftRev != req.ExpectedDraftRev ||
		!bytes.Equal(stage.BaseHash, current.BaseHash) {
		return conflict()
	}
	merged, conflicts, err := config.Merge(current.BaseYAML, current.Draft.YAML, stage.YAML, req.Resolutions)
	if err != nil {
		return invalidConfig(err.Error(), nil)
	}
	if len(conflicts) > 0 {
		issues := make([]config.Issue, 0, len(conflicts))
		for _, item := range conflicts {
			issues = append(issues, config.Issue{Severity: "error", Path: item.Path, Message: item.Message})
		}
		return &apiError{Status: http.StatusConflict, Code: "import_conflict",
			Message: "请选择如何处理每一处合并冲突。", Issues: issues}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hash []byte
	err = tx.QueryRowContext(ctx, "SELECT content_sha256 FROM profile_imports WHERE profile_id = ?", id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound()
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(stage.BaseHash, hash) {
		return conflict()
	}
	result, err := tx.ExecContext(ctx,
		"DELETE FROM import_stages WHERE id = ? AND owner_id = ? AND profile_id = ? AND expires_at > ?",
		stage.ID, owner, id, time.Now().Unix())
	if err := requireAffected(result, err, notFound); err != nil {
		return err
	}
	draft, err := s.saveDraft(ctx, tx, owner, id, req.ExpectedDraftRev, merged, merged != current.Draft.YAML)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE profile_imports SET base_yaml = ?, content_sha256 = ?, last_fetched_at = ?
		WHERE profile_id = ?`, s.seal(stage.YAML, sourcePurpose(id, "base")), digest(stage.YAML), time.Now().UnixMilli(), id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, draft)
	return nil
}
