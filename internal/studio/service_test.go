package studio

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"clash-rule-gen/internal/config"
)

const (
	apiPrefix     = "/api/v1"
	browserOrigin = "https://studio.example.invalid"
	testPassword  = "test-password-123"
)

type testClient struct {
	auth   *http.Cookie
	csrf   string
	origin string
}

func (c testClient) request(handler http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			panic(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.auth != nil {
		req.AddCookie(c.auth)
	}
	if c.origin != "" {
		req.Header.Set("Origin", c.origin)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func expect(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d — %s", rec.Code, status, rec.Body.String())
	}
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

func testOpen(t *testing.T, filename string, opts Options) (http.Handler, *sql.DB) {
	t.Helper()
	dsn := (&url.URL{Scheme: "file", Path: filename,
		RawQuery: "_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL&_txlock=immediate",
	}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if opts.MasterKey == nil {
		opts.MasterKey = bytes.Repeat([]byte{9}, 32)
	}
	if opts.PublicBaseURL == "" {
		opts.PublicBaseURL = browserOrigin
	}
	handler, err := New(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	return handler, db
}

func registerClient(t *testing.T, handler http.Handler, username string) testClient {
	t.Helper()
	client := testClient{origin: browserOrigin}
	rec := client.request(handler, http.MethodPost, apiPrefix+"/auth/register", credentials{username, testPassword}, nil)
	expect(t, rec, http.StatusCreated)
	user := decode[sessionUser](t, rec)
	if user.Username != username || user.CSRFToken == "" {
		t.Fatal("registration session missing")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("session cookie is missing expected attributes")
	}
	client.auth, client.csrf = cookies[0], user.CSRFToken
	return client
}

func createForTest(t *testing.T, handler http.Handler, client testClient, name string, yaml *string) Profile {
	t.Helper()
	rec := client.request(handler, http.MethodPost, apiPrefix+"/profiles", createProfileRequest{
		profileMetadata: profileMetadata{Name: name, Target: "both"}, YAML: yaml,
	}, nil)
	expect(t, rec, http.StatusCreated)
	return decode[Profile](t, rec)
}

func profileURL(id string) string { return apiPrefix + "/profiles/" + id }

func TestSessionCSRFAndProfileIsolation(t *testing.T) {
	handler, _ := testOpen(t, filepath.Join(t.TempDir(), "app.db"), Options{SecureCookies: true, AllowedOrigin: "http://localhost:5173"})
	visitor := testClient{}
	expect(t, visitor.request(handler, http.MethodGet, apiPrefix+"/auth/me", nil, nil), http.StatusUnauthorized)
	foreign := testClient{origin: "https://different.example.invalid"}
	expect(t, foreign.request(handler, http.MethodPost, apiPrefix+"/auth/register", credentials{"ForeignName", testPassword}, nil), http.StatusForbidden)
	alice := registerClient(t, handler, "Alice")
	if !alice.auth.Secure {
		t.Fatal("HTTPS deployments must set Secure on session cookies")
	}
	bob := registerClient(t, handler, "Bob")
	expect(t, visitor.request(handler, http.MethodPost, apiPrefix+"/auth/register", credentials{"alice", testPassword}, nil), http.StatusConflict)
	expect(t, visitor.request(handler, http.MethodPost, apiPrefix+"/auth/login", credentials{"Alice", "incorrect password"}, nil), http.StatusUnauthorized)

	home := createForTest(t, handler, alice, "家庭网关", nil)
	path := profileURL(home.ID)
	me := alice.request(handler, http.MethodGet, apiPrefix+"/auth/me", nil, nil)
	expect(t, me, http.StatusOK)
	if got := decode[sessionUser](t, me); got.Username != "Alice" || got.CSRFToken != alice.csrf {
		t.Fatal("session lookup changed authenticated user or token")
	}
	for _, suffix := range []string{"", "/draft", "/preview", "/export.yaml", "/links", "/versions"} {
		expect(t, bob.request(handler, http.MethodGet, path+suffix, nil, nil), http.StatusNotFound)
	}
	expect(t, bob.request(handler, http.MethodDelete, path, nil, nil), http.StatusNotFound)
	expect(t, testClient{auth: alice.auth}.request(handler, http.MethodDelete, path, nil, nil), http.StatusForbidden)
	expect(t, testClient{auth: alice.auth, csrf: alice.csrf, origin: foreign.origin}.request(handler, http.MethodDelete, path, nil, nil), http.StatusForbidden)

	aliceDev := alice
	aliceDev.origin = "http://localhost:5173"
	rec := aliceDev.request(handler, http.MethodPatch, path, profileMetadata{Name: "路由器配置", Target: "openclash"}, nil)
	expect(t, rec, http.StatusOK)
	if profile := decode[Profile](t, rec); profile.Name != "路由器配置" || profile.Target != "openclash" {
		t.Fatalf("profile metadata changed incorrectly: %+v", profile)
	}
	rec = alice.request(handler, http.MethodPost, path+"/duplicate", map[string]any{}, nil)
	expect(t, rec, http.StatusCreated)
	duplicate := decode[Profile](t, rec)
	if duplicate.ID == home.ID || !strings.HasSuffix(duplicate.Name, "（副本）") {
		t.Fatal("duplicate reused the source identity")
	}
	rec = alice.request(handler, http.MethodGet, apiPrefix+"/profiles?limit=1", nil, nil)
	expect(t, rec, http.StatusOK)
	if list := decode[struct {
		Profiles   []Profile
		NextOffset int
	}](t, rec); len(list.Profiles) != 1 || list.NextOffset != 1 {
		t.Fatal("profile pagination returns incorrect metadata")
	}
	rec = bob.request(handler, http.MethodGet, apiPrefix+"/profiles", nil, nil)
	expect(t, rec, http.StatusOK)
	if list := decode[map[string][]Profile](t, rec)["profiles"]; len(list) != 0 {
		t.Fatal("other account's profile leaked into list")
	}
	expect(t, alice.request(handler, http.MethodPost, apiPrefix+"/auth/logout", nil, nil), http.StatusNoContent)
	expect(t, alice.request(handler, http.MethodGet, path, nil, nil), http.StatusUnauthorized)

	rec = visitor.request(handler, http.MethodPost, apiPrefix+"/auth/login", credentials{"ALICE", testPassword}, nil)
	expect(t, rec, http.StatusOK)
	loggedIn := decode[sessionUser](t, rec)
	if loggedIn.Username != "Alice" || loggedIn.CSRFToken == alice.csrf {
		t.Fatal("login failed to normalize usernames or rotate the session")
	}
}

func patchFields(expected int64, changes map[string]any) map[string]any {
	return map[string]any{"expectedDraftRev": expected, "changes": changes}
}

func defaultLink(t *testing.T, handler http.Handler, client testClient, projectPath string) (Link, string) {
	t.Helper()
	rec := client.request(handler, http.MethodGet, projectPath+"/links", nil, nil)
	expect(t, rec, http.StatusOK)
	links := decode[map[string][]Link](t, rec)["links"]
	if len(links) != 1 {
		t.Fatalf("default link count = %d", len(links))
	}
	link := links[0]
	rec = client.request(handler, http.MethodGet, projectPath+"/links/"+link.ID+"/url", nil, nil)
	expect(t, rec, http.StatusOK)
	address := decode[map[string]string](t, rec)["url"]
	if !strings.HasPrefix(address, browserOrigin+"/s/") || !strings.HasSuffix(address, ".yaml") {
		t.Fatal("subscription uses incorrect scheme, host or suffix")
	}
	return link, address
}

func TestPublishSnapshotsAcrossRestart(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "storage.db")
	handler, db := testOpen(t, filename, Options{})
	client := registerClient(t, handler, "Publisher")
	raw := "# sample comments remain\nmixed-port: 7890\nmode: rule\nsecret: sample-test-controller-secret\nrules:\n  - MATCH,DIRECT\n"
	profile := createForTest(t, handler, client, "发布测试", &raw)
	path := profileURL(profile.ID)
	expect(t, client.request(handler, http.MethodPost, path+"/links", map[string]string{"label": "disabled"}, nil), http.StatusConflict)
	rec := client.request(handler, http.MethodPost, path+"/publish", revisionRequest{ExpectedDraftRev: 1}, nil)
	expect(t, rec, http.StatusOK)
	if version := decode[Version](t, rec); version.Version != 1 || version.DraftRev != 1 {
		t.Fatalf("first publish = %+v", version)
	}
	link, externalURL := defaultLink(t, handler, client, path)
	anonymous := testClient{}
	rec = anonymous.request(handler, http.MethodGet, externalURL, nil, nil)
	expect(t, rec, http.StatusOK)
	first := rec.Body.String()
	etag := rec.Header().Get("ETag")
	if !strings.Contains(first, "mixed-port: 7890") || !strings.Contains(first, "# sample comments remain") || etag == "" ||
		rec.Header().Get("Cache-Control") != "private, no-store" ||
		rec.Header().Get("Content-Type") != "application/yaml; charset=utf-8" {
		t.Fatal("subscription response lost YAML, cache controls or ETag")
	}
	rec = anonymous.request(handler, http.MethodHead, externalURL, nil, nil)
	expect(t, rec, http.StatusOK)
	if rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != strconv.Itoa(len(first)) {
		t.Fatal("HEAD needs HTTP metadata without body")
	}
	expect(t, anonymous.request(handler, http.MethodGet, externalURL, nil, map[string]string{"If-None-Match": "W/" + etag}), http.StatusNotModified)
	rec = client.request(handler, http.MethodPatch, path+"/draft", patchFields(1, map[string]any{"mixed-port": 7891}), nil)
	expect(t, rec, http.StatusOK)
	if current := decode[Draft](t, rec); current.DraftRev != 2 {
		t.Fatalf("draft revision = %d", current.DraftRev)
	}
	rec = anonymous.request(handler, http.MethodGet, externalURL, nil, nil)
	if rec.Body.String() != first {
		t.Fatal("draft changes leaked into published subscription")
	}
	expect(t, client.request(handler, http.MethodPost, path+"/publish", revisionRequest{ExpectedDraftRev: 2}, nil), http.StatusOK)
	rec = anonymous.request(handler, http.MethodGet, externalURL, nil, map[string]string{"If-None-Match": etag})
	expect(t, rec, http.StatusOK)
	second := rec.Body.String()
	if !strings.Contains(second, "mixed-port: 7891") || rec.Header().Get("ETag") == etag {
		t.Fatal("the same link did not load the new published revision")
	}
	// Replaying v1 publication must neither create a third version nor reset the pointer.
	expect(t, client.request(handler, http.MethodPost, path+"/publish", revisionRequest{ExpectedDraftRev: 1}, nil), http.StatusOK)
	if rec = anonymous.request(handler, http.MethodGet, externalURL, nil, nil); rec.Body.String() != second {
		t.Fatal("publication retry rolled back the active subscription")
	}
	rec = client.request(handler, http.MethodGet, path+"/versions", nil, nil)
	expect(t, rec, http.StatusOK)
	if history := decode[map[string][]Version](t, rec)["versions"]; len(history) != 2 || history[0].Version != 2 {
		t.Fatal("history lost ordering or duplicated a publish")
	}
	rec = client.request(handler, http.MethodPost, path+"/versions/1/restore", revisionRequest{ExpectedDraftRev: 2}, nil)
	expect(t, rec, http.StatusOK)
	if restored := decode[Draft](t, rec); restored.DraftRev != 3 || restored.YAML != first {
		t.Fatal("restore should copy the original snapshot to a new draft")
	}
	var sealedDraft, sealedVersion, sealedLink []byte
	if err := db.QueryRow(`
		SELECT p.draft_yaml, v.yaml, l.token FROM profiles p
		JOIN profile_versions v ON v.id = p.published_version_id
		JOIN subscription_links l ON l.profile_id = p.id WHERE p.id = ?`, profile.ID,
	).Scan(&sealedDraft, &sealedVersion, &sealedLink); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealedDraft, []byte("sample-test-controller-secret")) || bytes.Contains(sealedVersion, []byte("sample-test-controller-secret")) ||
		bytes.Contains(sealedLink, []byte(strings.TrimSuffix(strings.TrimPrefix(externalURL, browserOrigin+"/s/"), ".yaml"))) {
		t.Fatal("sensitive YAML or token stored in plaintext")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	handler, _ = testOpen(t, filename, Options{})
	rec = anonymous.request(handler, http.MethodGet, externalURL, nil, nil)
	expect(t, rec, http.StatusOK)
	if rec.Body.String() != second {
		t.Fatal("published snapshot was lost on restart")
	}
	rec = client.request(handler, http.MethodGet, path+"/draft", nil, nil)
	expect(t, rec, http.StatusOK)
	if restored := decode[Draft](t, rec); restored.DraftRev != 3 || restored.YAML != first {
		t.Fatal("authenticated draft was lost on restart")
	}
	rec = client.request(handler, http.MethodPost, path+"/links", map[string]string{"label": "桌面"}, nil)
	expect(t, rec, http.StatusCreated)
	if decode[Link](t, rec).ID == link.ID {
		t.Fatal("device link must be distinct")
	}
	expect(t, client.request(handler, http.MethodDelete, path+"/links/"+link.ID, nil, nil), http.StatusNoContent)
	expect(t, anonymous.request(handler, http.MethodGet, externalURL, nil, map[string]string{"If-None-Match": "*"}), http.StatusNotFound)
	expect(t, client.request(handler, http.MethodGet, path+"/links/"+link.ID+"/url", nil, nil), http.StatusNotFound)
	expect(t, client.request(handler, http.MethodDelete, path, nil, nil), http.StatusNoContent)
}

func TestDraftValidationAndRevisionConflict(t *testing.T) {
	handler, _ := testOpen(t, filepath.Join(t.TempDir(), "app.db"), Options{})
	client := registerClient(t, handler, "Editor")
	profile := createForTest(t, handler, client, "规则测试", nil)
	path := profileURL(profile.ID)
	rec := client.request(handler, http.MethodPatch, path+"/draft", patchFields(1, map[string]any{
		"rules": []string{"RULE-SET,missing,DIRECT", "MATCH,DIRECT"},
	}), nil)
	expect(t, rec, http.StatusOK)
	expect(t, client.request(handler, http.MethodGet, path+"/preview?rev=1", nil, nil), http.StatusConflict)
	rec = client.request(handler, http.MethodGet, path+"/preview?rev=2", nil, nil)
	expect(t, rec, http.StatusOK)
	var preview struct {
		Draft
		Issues []config.Issue
	}
	preview = decode[struct {
		Draft
		Issues []config.Issue
	}](t, rec)
	ok := false
	for _, issue := range preview.Issues {
		ok = ok || (issue.Severity == "error" && issue.Path == "rules[0]")
	}
	if !ok {
		t.Fatalf("preview lost rule diagnostics: %+v", preview.Issues)
	}
	expect(t, client.request(handler, http.MethodGet, path+"/export.yaml?rev=2", nil, nil), http.StatusUnprocessableEntity)
	expect(t, client.request(handler, http.MethodPost, path+"/publish", revisionRequest{ExpectedDraftRev: 2}, nil), http.StatusUnprocessableEntity)

	var wg sync.WaitGroup
	status := make(chan int, 2)
	for _, replacement := range [][]string{{"MATCH,DIRECT"}, {"DOMAIN,example.invalid,DIRECT", "MATCH,DIRECT"}} {
		wg.Add(1)
		go func(rules []string) {
			defer wg.Done()
			rec := client.request(handler, http.MethodPatch, path+"/draft", patchFields(2, map[string]any{"rules": rules}), nil)
			status <- rec.Code
		}(replacement)
	}
	wg.Wait()
	first, second := <-status, <-status
	if !((first == http.StatusOK && second == http.StatusConflict) ||
		(first == http.StatusConflict && second == http.StatusOK)) {
		t.Fatalf("concurrent revision writes produced %d and %d", first, second)
	}
	rec = client.request(handler, http.MethodGet, path+"/export.yaml?rev=3", nil, nil)
	expect(t, rec, http.StatusOK)
	if disposition := rec.Header().Get("Content-Disposition"); !strings.Contains(disposition, "attachment") || !strings.Contains(disposition, ".yaml") {
		t.Fatal("download missing attachment filename")
	}
	if _, err := config.Parse(rec.Body.String()); err != nil {
		t.Fatalf("download is invalid YAML: %v", err)
	}
}

func TestUpstreamImportPreviewAndRefresh(t *testing.T) {
	remoteYAML := "mixed-port: 7890\nmode: rule\nrules:\n  - MATCH,DIRECT\n"
	remoteURL := "https://input.example.invalid/config?secret=private-demo-token"
	fetch := func(_ context.Context, rawURL, userAgent string) (string, error) {
		switch {
		case rawURL == remoteURL && userAgent == "Studio-Test":
			return remoteYAML, nil
		case rawURL == "https://input.example.invalid/nodes":
			return "proxies:\n  - {name: test, type: socks5, server: s.example.invalid, port: 1080}\n", nil
		default:
			return "", errors.New("URL or User-Agent unexpectedly changed: " + rawURL)
		}
	}
	handler, _ := testOpen(t, filepath.Join(t.TempDir(), "app.db"), Options{Fetch: fetch})
	client := registerClient(t, handler, "Importer")
	another := registerClient(t, handler, "Another")
	probe := apiPrefix + "/imports/probe"
	rec := client.request(handler, http.MethodPost, probe, probeRequest{Mode: "provider", URL: "https://input.example.invalid/nodes"}, nil)
	expect(t, rec, http.StatusOK)
	if out := decode[map[string]any](t, rec); out["kind"] != "provider" || out["yaml"] != nil || out["id"] != nil ||
		len(out["issues"].([]any)) != 0 {
		t.Fatal("provider probe should return summary only")
	}
	rec = client.request(handler, http.MethodPost, probe, probeRequest{Mode: "config", URL: remoteURL}, nil)
	expect(t, rec, http.StatusUnprocessableEntity)
	if bytes.Contains(rec.Body.Bytes(), []byte("private-demo-token")) {
		t.Fatal("injected fetch error leaked the full subscription URL")
	}
	rec = client.request(handler, http.MethodPost, probe, probeRequest{Mode: "config", URL: remoteURL, UserAgent: "Studio-Test"}, nil)
	expect(t, rec, http.StatusOK)
	stage := decode[map[string]json.RawMessage](t, rec)
	if bytes.Contains(rec.Body.Bytes(), []byte("private-demo-token")) || string(stage["host"]) != `"input.example.invalid"` {
		t.Fatal("probe should display upstream host without URL credentials")
	}
	var stageID string
	if err := json.Unmarshal(stage["id"], &stageID); err != nil || stageID == "" {
		t.Fatal("probe omitted import ID")
	}
	params := createProfileRequest{
		profileMetadata: profileMetadata{Name: "远程订阅", Target: "openclash"}, ImportID: stageID,
	}
	expect(t, another.request(handler, http.MethodPost, apiPrefix+"/profiles", params, nil), http.StatusNotFound)
	rec = client.request(handler, http.MethodPost, apiPrefix+"/profiles", params, nil)
	expect(t, rec, http.StatusCreated)
	profile := decode[Profile](t, rec)
	if !profile.HasSource {
		t.Fatal("the import baseline was not linked to the new profile")
	}
	expect(t, client.request(handler, http.MethodPost, apiPrefix+"/profiles", params, nil), http.StatusNotFound)

	path := profileURL(profile.ID)
	expect(t, client.request(handler, http.MethodPatch, path+"/draft", patchFields(1, map[string]any{
		"mixed-port": 7891, "allow-lan": false,
	}), nil), http.StatusOK)
	remoteYAML = "mixed-port: 7892\nmode: rule\nlog-level: warning\nrules:\n  - MATCH,DIRECT\n"
	refresh := path + "/imports/refresh"
	expect(t, another.request(handler, http.MethodPost, refresh, refreshRequest{Action: "preview"}, nil), http.StatusNotFound)
	rec = client.request(handler, http.MethodPost, refresh, refreshRequest{Action: "preview"}, nil)
	expect(t, rec, http.StatusOK)
	preview := decode[struct {
		ID        string
		Conflicts []config.Conflict
	}](t, rec)
	if len(preview.Conflicts) != 1 || preview.Conflicts[0].Path != "mixed-port" {
		t.Fatalf("refresh conflicts = %+v", preview.Conflicts)
	}
	rec = client.request(handler, http.MethodPost, refresh, refreshRequest{
		Action: "apply", ImportID: preview.ID, revisionRequest: revisionRequest{ExpectedDraftRev: 2},
	}, nil)
	expect(t, rec, http.StatusConflict)
	rec = client.request(handler, http.MethodPost, refresh, refreshRequest{
		Action: "apply", ImportID: preview.ID, revisionRequest: revisionRequest{ExpectedDraftRev: 2},
		Resolutions: map[string]string{"mixed-port": "upstream"},
	}, nil)
	expect(t, rec, http.StatusOK)
	draft := decode[Draft](t, rec)
	if draft.DraftRev != 3 || !strings.Contains(draft.YAML, "mixed-port: 7892") ||
		!strings.Contains(draft.YAML, "allow-lan: false") || !strings.Contains(draft.YAML, "log-level: warning") {
		t.Fatal("three-way refresh did not preserve the right fields")
	}
	expect(t, client.request(handler, http.MethodPost, refresh, refreshRequest{
		Action: "apply", ImportID: preview.ID, revisionRequest: revisionRequest{ExpectedDraftRev: 3},
	}, nil), http.StatusNotFound)
}

func TestDefaultFetcherReportsSanitizedReason(t *testing.T) {
	handler, _ := testOpen(t, filepath.Join(t.TempDir(), "failure.db"), Options{})
	client := registerClient(t, handler, "FetchTest")
	rec := client.request(handler, http.MethodPost, apiPrefix+"/imports/probe", probeRequest{
		Mode: "config", URL: "http://127.0.0.1:1/sub?secret=private-demo-token",
	}, nil)
	expect(t, rec, http.StatusUnprocessableEntity)
	if problem := decode[apiError](t, rec); problem.Code != "source_unavailable" || !strings.Contains(problem.Message, "公网服务器") ||
		strings.Contains(problem.Message, "private-demo-token") {
		t.Fatal("built-in fetcher should report the sanitized cause")
	}
}
