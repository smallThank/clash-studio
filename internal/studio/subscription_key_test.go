package studio

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubscriptionKeysRequireMatchingProfileAndRotate(t *testing.T) {
	handler, _ := testOpen(t, filepath.Join(t.TempDir(), "keys.db"), Options{})
	owner := registerClient(t, handler, "KeyOwner")
	other := registerClient(t, handler, "KeyOutsider")
	visitor := testClient{}
	profile := createForTest(t, handler, owner, "家庭订阅", nil)
	path := profileURL(profile.ID)
	keyPath := path + "/subscription-key"
	expect(t, visitor.request(handler, http.MethodGet, keyPath, nil, nil), http.StatusUnauthorized)
	expect(t, other.request(handler, http.MethodGet, keyPath, nil, nil), http.StatusNotFound)
	expect(t, other.request(handler, http.MethodPost, keyPath+"/rotate", nil, nil), http.StatusNotFound)
	keyResponse := owner.request(handler, http.MethodGet, keyPath, nil, nil)
	expect(t, keyResponse, http.StatusOK)
	firstKey := decode[subscriptionKey](t, keyResponse)
	if !validToken(firstKey.Key) || keyResponse.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("missing protected profile key")
	}
	rec := owner.request(handler, http.MethodPost, path+"/duplicate", map[string]any{}, nil)
	expect(t, rec, http.StatusCreated)
	copyPath := profileURL(decode[Profile](t, rec).ID)
	rec = owner.request(handler, http.MethodGet, copyPath+"/subscription-key", nil, nil)
	expect(t, rec, http.StatusOK)
	copyKey := decode[subscriptionKey](t, rec).Key
	if copyKey == firstKey.Key {
		t.Fatal("copy inherited the original key")
	}

	expect(t, owner.request(handler, http.MethodPost, path+"/publish", revisionRequest{ExpectedDraftRev: 1}, nil), http.StatusOK)
	link, fullURL := defaultLink(t, handler, owner, path)
	endpoint, err := url.Parse(fullURL)
	if err != nil || endpoint.Query().Get("key") != firstKey.Key {
		t.Fatal("generated subscription URL did not contain the managed key")
	}
	endpoint.RawQuery = ""
	plain := endpoint.String()
	for _, test := range []struct {
		path   string
		header string
	}{
		{path: plain},
		{path: plain, header: "Bearer " + copyKey},
		{path: fullURL, header: "Bearer " + firstKey.Key},
		{path: fullURL + "&key=" + firstKey.Key},
	} {
		headers := map[string]string{"If-None-Match": "*"}
		if test.header != "" {
			headers["Authorization"] = test.header
		}
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rec := visitor.request(handler, method, test.path, nil, headers)
			expect(t, rec, http.StatusNotFound)
			if rec.Header().Get("ETag") != "" {
				t.Fatal("unauthorized response leaked version metadata")
			}
		}
	}
	expect(t, visitor.request(handler, http.MethodGet, plain, nil, map[string]string{"Authorization": "Bearer " + firstKey.Key}), http.StatusOK)

	noCSRF := owner
	noCSRF.csrf = ""
	expect(t, noCSRF.request(handler, http.MethodPost, keyPath+"/rotate", nil, nil), http.StatusForbidden)
	rec = owner.request(handler, http.MethodPost, keyPath+"/rotate", nil, nil)
	expect(t, rec, http.StatusOK)
	newKey := decode[subscriptionKey](t, rec).Key
	if !validToken(newKey) || newKey == firstKey.Key {
		t.Fatal("key did not rotate")
	}
	expect(t, visitor.request(handler, http.MethodGet, fullURL, nil, nil), http.StatusNotFound)
	updatedLink, updatedURL := defaultLink(t, handler, owner, path)
	if updatedLink.ID != link.ID || updatedURL != plain+"?key="+newKey {
		t.Fatal("rotation changed the link ID or failed to use the new key")
	}
	expect(t, visitor.request(handler, http.MethodGet, updatedURL, nil, nil), http.StatusOK)
}

func TestSubscriptionKeyMigrationKeepsPublishedDataAndLinkMetadata(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "legacy.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	key := bytes.Repeat([]byte{7}, 32)
	aead, err := newAEAD(key)
	if err != nil {
		t.Fatal(err)
	}
	legacy := &Server{aead: aead}
	token := randomToken(32)
	yaml := "rules:\n  - MATCH,DIRECT\n"
	link := randomToken(18)
	if _, err := db.Exec(schema + "PRAGMA user_version = 1;"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users VALUES('owner','Owner','owner',X'01',1);
		INSERT INTO profiles VALUES('profile','owner','配置','both',?,1,NULL,1);
		INSERT INTO profile_versions VALUES('version','profile',1,1,?,?,1);
		UPDATE profiles SET published_version_id = 'version' WHERE id = 'profile';
		INSERT INTO subscription_links VALUES(?, 'profile','设备',?,?,2,NULL);
		INSERT INTO subscription_links VALUES('revoked','profile','旧设备',X'ff',X'ff',3,4);`,
		legacy.seal(yaml, draftPurpose("profile")), legacy.seal(yaml, versionPurpose("version")), digest(yaml),
		link, digest(token), legacy.seal(token, "link:"+link+":token")); err != nil {
		t.Fatal(err)
	}
	handler, err := New(db, Options{MasterKey: key})
	if err != nil {
		t.Fatal(err)
	}
	var sealed []byte
	if err := db.QueryRow("SELECT sealed_key FROM profile_subscription_keys WHERE profile_id = 'profile'").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	migrated, err := legacy.unseal(sealed, subscriptionKeyPurpose("profile"))
	if err != nil || !validToken(migrated) {
		t.Fatalf("invalid migrated key: %v", err)
	}
	handler, err = New(db, Options{MasterKey: key})
	if err != nil {
		t.Fatal(err)
	}
	visitor := testClient{}
	expect(t, visitor.request(handler, http.MethodGet, "/s/"+token+".yaml", nil, nil), http.StatusNotFound)
	result := visitor.request(handler, http.MethodGet, "/s/"+link+".yaml?key="+migrated, nil, nil)
	expect(t, result, http.StatusOK)
	if result.Body.String() != yaml {
		t.Fatal("migration discarded the published version")
	}
	var revoked int64
	if err := db.QueryRow("SELECT revoked_at FROM subscription_links WHERE id = 'revoked'").Scan(&revoked); err != nil || revoked != 4 {
		t.Fatalf("migration lost the revocation: %d, %v", revoked, err)
	}
}

func TestSubscriptionURLWithAutomaticPublicAddress(t *testing.T) {
	_, db := testOpen(t, filepath.Join(t.TempDir(), "automatic.db"), Options{})
	masterKey := bytes.Repeat([]byte{9}, 32)
	handler, err := New(db, Options{MasterKey: masterKey})
	if err != nil {
		t.Fatal(err)
	}
	misdirected := testClient{origin: "https://other.example.com:8443"}
	proxyHeaders := map[string]string{"Sec-Fetch-Site": "same-origin", "X-Forwarded-Host": "other.example.com", "X-Forwarded-Proto": "https"}
	expect(t, misdirected.request(handler, http.MethodPost, "http://clash.example.com:8443"+apiPrefix+"/auth/register",
		credentials{"Misdirected", testPassword}, proxyHeaders), http.StatusForbidden)

	client := testClient{origin: "https://clash.example.com:8443"}
	base := "http://clash.example.com:8443" + apiPrefix
	rec := client.request(handler, http.MethodPost, base+"/auth/register", credentials{"Owner", testPassword}, proxyHeaders)
	expect(t, rec, http.StatusCreated)
	client.csrf = decode[sessionUser](t, rec).CSRFToken
	client.auth = rec.Result().Cookies()[0]
	if !client.auth.Secure {
		t.Fatal("HTTPS access must create a Secure cookie")
	}
	rec = client.request(handler, http.MethodPost, base+"/profiles",
		createProfileRequest{profileMetadata: profileMetadata{Name: "测试", Target: "both"}}, proxyHeaders)
	expect(t, rec, http.StatusCreated)
	id := decode[Profile](t, rec).ID
	path := base + "/profiles/" + id
	expect(t, client.request(handler, http.MethodPost, path+"/publish", revisionRequest{ExpectedDraftRev: 1}, proxyHeaders), http.StatusOK)
	rec = client.request(handler, http.MethodGet, path+"/links", nil, nil)
	expect(t, rec, http.StatusOK)
	link := decode[map[string][]Link](t, rec)["links"][0]
	suffix := "/s/" + link.ID + ".yaml"
	rec = client.request(handler, http.MethodGet, path+"/links/"+link.ID+"/url?relative=1", nil, proxyHeaders)
	expect(t, rec, http.StatusOK)
	result := decode[map[string]string](t, rec)
	if key, ok := strings.CutPrefix(result["url"], suffix+"?key="); result["endpoint"] != suffix || !ok || !validToken(key) {
		t.Fatal("automatic browser URL should have no server or proxy origin")
	}
	linkPath := "/profiles/" + id + "/links/" + link.ID + "/url"
	rec = client.request(handler, http.MethodGet, "http://192.0.2.10:8081"+apiPrefix+linkPath, nil, proxyHeaders)
	expect(t, rec, http.StatusOK)
	if result := decode[map[string]string](t, rec); result["endpoint"] != "http://192.0.2.10:8081"+suffix {
		t.Fatal("direct request did not use HTTP Host and port")
	}
	handler, err = New(db, Options{MasterKey: masterKey, PublicBaseURL: "https://subscription.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	rec = client.request(handler, http.MethodGet, path+"/links/"+link.ID+"/url?relative=1", nil, nil)
	expect(t, rec, http.StatusOK)
	if result := decode[map[string]string](t, rec); result["endpoint"] != "https://subscription.example.com"+suffix {
		t.Fatal("explicit public base must take priority")
	}
}
