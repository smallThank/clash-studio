package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"clash-rule-gen/internal/studio"
)

const testOrigin = "https://studio.example.test"

const acceptanceYAML = `
# 局域网的 Mihomo 配置
mixed-port: 7890
mode: rule
log-level: info
dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver:
    - 1.1.1.1
proxy-providers:
  示例订阅:
    type: inline
    payload:
      - {name: 内联节点, type: socks5, server: 192.0.2.10, port: 1080}
proxies:
  - name: 拨号节点
    type: socks5
    server: 192.0.2.20
    port: 1080
  - name: 出口节点
    type: socks5
    server: 192.0.2.30
    port: 1080
    username: fixture
    password: synthetic-proxy-secret-7a6c9
    dialer-proxy: 拨号节点
proxy-groups:
  - name: 节点选择
    type: select
    proxies: [出口节点, DIRECT]
    use: [示例订阅]
rule-providers:
  示例规则集:
    type: inline
    behavior: domain
    payload: [+.example.org]
rules:
  - RULE-SET,示例规则集,DIRECT
  - DOMAIN-SUFFIX,example.net,DIRECT
  - MATCH,节点选择
sniffer:
  enable: false
`

func TestMasterKeyPersistsAcrossStartup(t *testing.T) {
	dir := t.TempDir()
	initial, err := loadMasterKey(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := loadMasterKey(dir, "")
	if err != nil || !bytes.Equal(initial, reopened) {
		t.Fatalf("master key changed after reopening: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "master.key"))
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("master key file has unsafe permissions: %v, %v", info, err)
	}
	if _, err := loadMasterKey(dir, "invalid"); err == nil {
		t.Fatal("invalid override was accepted")
	}
}

func TestSiteRoutes(t *testing.T) {
	app := siteHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), fstest.MapFS{
		"index.html":    {Data: []byte("<html>Clash 配置台</html>")},
		"assets/app.js": {Data: []byte("export {}")},
	})
	for _, test := range []struct {
		path  string
		code  int
		cache string
	}{
		{"/", 200, "no-cache"},
		{"/assets/app.js", 200, "public, max-age=31536000, immutable"},
		{"/assets/", 404, ""},
		{"/missing.js", 404, ""},
		{"/healthz", 200, "no-store"},
		{"/api/v1/auth/me", 204, ""},
		{"/s/no-token.yaml", 204, ""},
	} {
		response := call(t, app, http.MethodGet, test.path, nil, nil, nil)
		if response.Code != test.code || response.Header().Get("Cache-Control") != test.cache {
			t.Errorf("%s returned %d %q, want %d %q", test.path, response.Code, response.Header().Get("Cache-Control"), test.code, test.cache)
		}
	}
	emptySite := siteHandler(http.NotFoundHandler(), fstest.MapFS{})
	expect(t, call(t, emptySite, http.MethodGet, "/", nil, nil, nil), http.StatusServiceUnavailable)
}

type testSession struct {
	cookie *http.Cookie
	csrf   string
}

type profileState struct {
	ID               string `json:"id"`
	DraftRev         int64  `json:"draftRev"`
	PublishedVersion int    `json:"publishedVersion"`
	HasSource        bool   `json:"hasSource"`
}

type draftState struct {
	DraftRev int64  `json:"draftRev"`
	YAML     string `json:"yaml"`
}

func TestApplicationWorkflow(t *testing.T) {
	dir := t.TempDir()
	database := filepath.Join(dir, "studio.sqlite")
	key, err := loadMasterKey(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	upstream := acceptanceYAML
	opts := studio.Options{
		MasterKey: key, PublicBaseURL: testOrigin, SecureCookies: true,
		Fetch: func(_ context.Context, source, agent string) (string, error) {
			if source != "https://upstream.example.test/clash.yaml?key=synthetic-import-token" || agent != "FixtureClient/1" {
				return "", fmt.Errorf("unexpected source parameters")
			}
			return upstream, nil
		},
	}
	db, err := openDB(database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := studio.New(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	app := siteHandler(service, fstest.MapFS{"index.html": {Data: []byte("test frontend")}})
	owner := register(t, app, "owner-one")
	outsider := register(t, app, "other-user")

	var staged struct {
		ID   string `json:"id"`
		YAML string `json:"yaml"`
	}
	post(t, app, "/api/v1/imports/probe", &owner, map[string]any{
		"mode": "config", "url": "https://upstream.example.test/clash.yaml?key=synthetic-import-token",
		"userAgent": "FixtureClient/1",
	}, http.StatusOK, &staged)
	if staged.ID == "" || !strings.Contains(staged.YAML, "dialer-proxy") {
		t.Fatal("import preview missing")
	}

	var profile profileState
	post(t, app, "/api/v1/profiles", &owner, map[string]any{
		"name": "家庭配置", "target": "both", "importId": staged.ID,
	}, http.StatusCreated, &profile)
	if !profile.HasSource || profile.ID == "" {
		t.Fatal("source provenance missing")
	}
	root := "/api/v1/profiles/" + profile.ID
	expect(t, call(t, app, http.MethodGet, root+"/draft", nil, &outsider, nil), http.StatusNotFound)

	var edited draftState
	patchBody := map[string]any{
		"expectedDraftRev": profile.DraftRev,
		"changes": map[string]any{"dns": map[string]any{
			"enable": true, "enhanced-mode": "fake-ip", "nameserver": []string{"9.9.9.9"},
		}},
	}
	edit := call(t, app, http.MethodPatch, root+"/draft", patchBody, &owner, nil)
	expect(t, edit, http.StatusOK)
	decode(t, edit, &edited)
	if edited.DraftRev <= profile.DraftRev || !strings.Contains(edited.YAML, "9.9.9.9") ||
		!strings.Contains(edited.YAML, "# 局域网的 Mihomo 配置") || !strings.Contains(edited.YAML, "use:") {
		t.Fatal("editing DNS must preserve unaffected document structure")
	}
	expect(t, call(t, app, http.MethodPatch, root+"/draft", patchBody, &owner, nil), http.StatusConflict)

	var published struct {
		Version int `json:"version"`
	}
	postSuccess(t, app, root+"/publish", &owner, map[string]any{"expectedDraftRev": edited.DraftRev}, &published)
	if published.Version != 1 {
		t.Fatalf("first release version %d", published.Version)
	}
	var links struct {
		Links []struct {
			ID string `json:"id"`
		} `json:"links"`
	}
	response := call(t, app, http.MethodGet, root+"/links", nil, &owner, nil)
	expect(t, response, http.StatusOK)
	decode(t, response, &links)
	if len(links.Links) != 1 {
		t.Fatalf("expected default link, got %d", len(links.Links))
	}
	linkPath := root + "/links/" + links.Links[0].ID
	var url struct {
		URL string `json:"url"`
	}
	response = call(t, app, http.MethodGet, linkPath+"/url", nil, &owner, nil)
	expect(t, response, http.StatusOK)
	decode(t, response, &url)
	if !strings.HasPrefix(url.URL, testOrigin+"/s/") {
		t.Fatal("subscription link uses the wrong public endpoint")
	}
	path := strings.TrimPrefix(url.URL, testOrigin)
	first := call(t, app, http.MethodGet, path, nil, nil, nil)
	expect(t, first, http.StatusOK)
	if !strings.HasPrefix(first.Header().Get("Content-Type"), "application/yaml") ||
		first.Header().Get("Cache-Control") != "private, no-store" ||
		!strings.Contains(first.Body.String(), "9.9.9.9") {
		t.Fatal("public response missing YAML and cache headers")
	}
	for _, agent := range []string{"Clash.Meta", "clash-verge/v2.5.10"} {
		client := call(t, app, http.MethodGet, path, nil, nil, map[string]string{"User-Agent": agent})
		expect(t, client, http.StatusOK)
		if !bytes.Equal(client.Body.Bytes(), first.Body.Bytes()) {
			t.Errorf("subscription changed for User-Agent %s", agent)
		}
	}
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("subscription ETag missing")
	}
	expect(t, call(t, app, http.MethodGet, path, nil, nil, map[string]string{"If-None-Match": tag}), http.StatusNotModified)
	head := call(t, app, http.MethodHead, path, nil, nil, nil)
	expect(t, head, http.StatusOK)
	if head.Body.Len() != 0 || head.Header().Get("ETag") != tag {
		t.Fatal("HEAD must return metadata without the document")
	}

	upstream = strings.Replace(acceptanceYAML, "log-level: info", "log-level: warning", 1)
	var refresh struct {
		ID        string            `json:"id"`
		Conflicts []json.RawMessage `json:"conflicts"`
	}
	post(t, app, root+"/imports/refresh", &owner, map[string]any{"action": "preview"}, http.StatusOK, &refresh)
	if refresh.ID == "" || len(refresh.Conflicts) != 0 {
		t.Fatalf("independent changes conflict: %d", len(refresh.Conflicts))
	}
	post(t, app, root+"/imports/refresh", &owner, map[string]any{
		"action": "apply", "importId": refresh.ID, "expectedDraftRev": edited.DraftRev,
	}, http.StatusOK, &edited)
	if !strings.Contains(edited.YAML, "warning") || !strings.Contains(edited.YAML, "9.9.9.9") {
		t.Fatal("refresh did not preserve independent upstream and local changes")
	}
	stillFirst := call(t, app, http.MethodGet, path, nil, nil, nil)
	if !bytes.Equal(first.Body.Bytes(), stillFirst.Body.Bytes()) {
		t.Fatal("a draft update changed the subscription")
	}
	postSuccess(t, app, root+"/publish", &owner, map[string]any{"expectedDraftRev": edited.DraftRev}, &published)
	second := call(t, app, http.MethodGet, path, nil, nil, map[string]string{"If-None-Match": tag})
	expect(t, second, http.StatusOK)
	if published.Version != 2 || !strings.Contains(second.Body.String(), "warning") || second.Header().Get("ETag") == tag {
		t.Fatal("same link did not serve the second version")
	}
	exported := call(t, app, http.MethodGet, fmt.Sprintf("%s/export.yaml?rev=%d", root, edited.DraftRev), nil, &owner, nil)
	expect(t, exported, http.StatusOK)
	if !strings.Contains(exported.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("export missing its download filename")
	}
	checkWithMihomo(t, exported.Body.Bytes())

	post(t, app, root+"/versions/1/restore", &owner, map[string]any{"expectedDraftRev": edited.DraftRev}, http.StatusOK, &edited)
	if !strings.Contains(edited.YAML, "log-level: info") {
		t.Fatal("restore must copy the old document to the draft")
	}
	expect(t, call(t, app, http.MethodDelete, linkPath, nil, &owner, nil), http.StatusNoContent)
	expect(t, call(t, app, http.MethodGet, path, nil, nil, map[string]string{"If-None-Match": tag}), http.StatusNotFound)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"synthetic-proxy-secret-7a6c9", "synthetic-import-token", "testing-password-92", owner.cookie.Value} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("SQLite contains a plaintext credential")
		}
	}
	reopened, err := openDB(database)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := studio.New(reopened, opts)
	if err != nil {
		t.Fatal(err)
	}
	afterRestart := call(t, restarted, http.MethodGet, root+"/draft", nil, &owner, nil)
	expect(t, afterRestart, http.StatusOK)
	if !strings.Contains(afterRestart.Body.String(), "log-level: info") {
		t.Fatal("draft did not survive restart")
	}
	expect(t, call(t, restarted, http.MethodGet, path, nil, nil, nil), http.StatusNotFound)
}

func register(t *testing.T, app http.Handler, username string) testSession {
	t.Helper()
	result := call(t, app, http.MethodPost, "/api/v1/auth/register",
		map[string]string{"username": username, "password": "testing-password-92"}, nil, nil)
	if result.Code != http.StatusCreated && result.Code != http.StatusOK {
		t.Fatalf("registration failed with %d: %s", result.Code, result.Body.String())
	}
	var body struct {
		CSRFToken string `json:"csrfToken"`
	}
	decode(t, result, &body)
	for _, cookie := range result.Result().Cookies() {
		if cookie.Name == "studio_session" && cookie.HttpOnly && cookie.Secure && body.CSRFToken != "" {
			return testSession{cookie, body.CSRFToken}
		}
	}
	t.Fatal("registration not accompanied by a secure session")
	return testSession{}
}

func call(t *testing.T, app http.Handler, method, path string, body any, session *testSession, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buffer bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buffer).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, testOrigin+path, &buffer)
	request.Header.Set("Origin", testOrigin)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if session != nil {
		request.AddCookie(session.cookie)
		request.Header.Set("X-CSRF-Token", session.csrf)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	out := httptest.NewRecorder()
	app.ServeHTTP(out, request)
	return out
}

func post(t *testing.T, app http.Handler, path string, session *testSession, body any, status int, target any) {
	t.Helper()
	response := call(t, app, http.MethodPost, path, body, session, nil)
	expect(t, response, status)
	decode(t, response, target)
}

func postSuccess(t *testing.T, app http.Handler, path string, session *testSession, body, target any) {
	t.Helper()
	response := call(t, app, http.MethodPost, path, body, session, nil)
	if response.Code != http.StatusOK && response.Code != http.StatusCreated {
		t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
	}
	decode(t, response, target)
}

func expect(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("returned %d, want %d: %s", response.Code, status, response.Body.String())
	}
}

func decode(t *testing.T, response *httptest.ResponseRecorder, value any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), value); err != nil {
		t.Fatal(err)
	}
}

func checkWithMihomo(t *testing.T, data []byte) {
	t.Helper()
	core := os.Getenv("MIHOMO_BIN")
	if core == "" {
		return
	}
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(core, "-t", "-d", dir, "-f", filename).CombinedOutput()
	if err != nil {
		t.Fatalf("Mihomo validation: %v\n%s", err, out)
	}
}
