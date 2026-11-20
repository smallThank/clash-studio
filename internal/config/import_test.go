package config

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestParseImportFormats(t *testing.T) {
	for _, test := range []struct {
		mode, input string
		valid       bool
	}{
		{"config", "dns: {enable: false}", true},
		{"config", "tun: {enable: true}", true},
		{"config", "some-html: not a configuration", false},
		{"provider", "proxies: [{name: Test, type: socks5}]", true},
		{"provider", "proxies: []", false},
		{"provider", "proxies: [{name: incomplete}]", false},
		{"provider", "proxy-groups: []", false},
		{"provider", "ss://encoded-placeholder", false},
		{"unknown", "mode: rule", false},
	} {
		_, err := ParseImport(test.input, test.mode)
		if (err == nil) != test.valid {
			t.Errorf("ParseImport(%q, %q) = %v, valid %t", test.input, test.mode, err, test.valid)
		}
	}
}

func TestParseImportReportsEncryptedAndEncodedResponses(t *testing.T) {
	armor := "\ufeff-----BEGIN AGE ENCRYPTED FILE-----\nfixture\n-----END AGE ENCRYPTED FILE-----"
	for _, test := range []struct{ mode, body, want string }{
		{"provider", armor, "age-secret-key"},
		{"config", armor, "本地解密"},
		{"provider", "ss://synthetic-placeholder", "节点链接列表"},
		{"provider", base64.StdEncoding.EncodeToString([]byte("vmess://synthetic-placeholder")), "Base64 编码"},
		{"provider", "unrecognized-ciphertext", "确认服务商使用的加密格式"},
	} {
		if _, err := ParseImport(test.body, test.mode); err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "synthetic-placeholder") {
			t.Errorf("%s response did not report %q safely: %v", test.mode, test.want, err)
		}
	}
}

func TestRemoteImportGuards(t *testing.T) {
	for _, host := range []string{
		"127.0.0.1", "[::1]", "10.0.0.1", "169.254.1.1", "192.168.1.1",
		"100.64.0.1", "192.0.2.1", "198.18.0.1", "224.0.0.1",
		"[2002:7f00:0001::]", "[64:ff9b::7f00:1]", "[fd00::1]",
		"localhost", "service.local", "local-machine", "localhost.test.localhost",
	} {
		if _, err := importURL("http://" + host + "/private"); !errors.Is(err, errNonPublic) {
			t.Errorf("%s should be rejected before network access: %v", host, err)
		}
	}
	if publicAddress(netip.MustParseAddr("::ffff:127.0.0.1")) {
		t.Fatal("IPv4-mapped loopback should be rejected before dialing")
	}
	for _, source := range []string{"ftp://example.invalid/", "https://[::ffff:7f00:1]/", "https://user:secret-placeholder@example.invalid/", "http://example.invalid/#fragment"} {
		if _, err := importURL(source); err == nil {
			t.Errorf("unexpectedly accepted invalid URL")
		}
	}
	ctx := context.Background()
	resolver := fakeResolver([]netip.Addr{netip.MustParseAddr("2606:4700::1111"), netip.MustParseAddr("10.0.0.1")})
	if _, err := publicAddresses(ctx, resolver, nil, "example.invalid"); !errors.Is(err, errNonPublic) {
		t.Fatalf("mixed DNS response should be blocked before dialing: %v", err)
	}
	resolver = fakeResolver([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2606:4700::1111")})
	addrs, err := publicAddresses(ctx, resolver, nil, "example.invalid")
	if err != nil || len(addrs) != 2 {
		t.Fatalf("expected validated public DNS results, got %d, %v", len(addrs), err)
	}
	target, _ := url.Parse("https://redirect.example.invalid/file")
	redirect := &http.Request{URL: target, Header: http.Header{"Referer": {"https://source.example.invalid/?credential-placeholder"}}}
	if err := checkRedirect(redirect, nil); err != nil || redirect.Header.Get("Referer") != "" {
		t.Fatalf("public redirect retains sensitive referrer or was rejected: %v", err)
	}
	unsafe, _ := url.Parse("https://127.0.0.1/internal")
	redirect.URL = unsafe
	if err := checkRedirect(redirect, nil); !errors.Is(err, errNonPublic) {
		t.Fatalf("blocked redirect was accepted: %v", err)
	}
	if err := checkRedirect(redirect, make([]*http.Request, 5)); !errors.Is(err, errRedirect) {
		t.Fatalf("redirect budget was not enforced: %v", err)
	}
}

func TestFakeIPUsesValidatedPublicDNSFallback(t *testing.T) {
	fake := netip.MustParseAddr("198.18.4.2")
	otherFake := netip.MustParseAddr("::ffff:198.19.0.3")
	ipv4 := netip.MustParseAddr("8.8.4.4")
	ipv6 := netip.MustParseAddr("2606:4700::1111")
	private := netip.MustParseAddr("192.168.1.1")
	for _, test := range []struct {
		name            string
		local, fallback []netip.Addr
		fallbackError   error
		calls           int
		want            []netip.Addr
		wantError       error
	}{
		{name: "public local DNS", local: []netip.Addr{ipv4}, want: []netip.Addr{ipv4}},
		{name: "fake IP", local: []netip.Addr{fake}, fallback: []netip.Addr{ipv4, ipv6}, calls: 1, want: []netip.Addr{ipv4, ipv6}},
		{name: "mixed fake and public", local: []netip.Addr{otherFake, ipv6}, fallback: []netip.Addr{ipv4}, calls: 1, want: []netip.Addr{ipv4}},
		{name: "local private beside fake IP", local: []netip.Addr{fake, private}, wantError: errNonPublic},
		{name: "fallback private beside public", local: []netip.Addr{fake}, fallback: []netip.Addr{ipv4, private}, calls: 1, wantError: errNonPublic},
		{name: "fallback still fake", local: []netip.Addr{fake}, fallback: []netip.Addr{otherFake}, calls: 1, wantError: errNonPublic},
		{name: "fallback unavailable", local: []netip.Addr{fake}, fallbackError: errors.New("network error"), calls: 1, wantError: errFakeIPResolution},
		{name: "fallback empty", local: []netip.Addr{fake}, calls: 1, wantError: errFakeIPResolution},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			fallback := resolverFunc(func(_ context.Context, network, host string) ([]netip.Addr, error) {
				calls++
				if network != "ip" || host != "subscription.example" {
					t.Errorf("unexpected fallback query %s %s", network, host)
				}
				return test.fallback, test.fallbackError
			})
			got, err := publicAddresses(context.Background(), fakeResolver(test.local), fallback, "subscription.example")
			if !errors.Is(err, test.wantError) || !slices.Equal(got, test.want) || calls != test.calls {
				t.Fatalf("got addresses %v, error %v and %d fallback calls", got, err, calls)
			}
		})
	}
}

func TestFetchYAMLResponseLimitsAndCredentialRedaction(t *testing.T) {
	request, err := http.NewRequest("GET", "https://subscribe.example.invalid/?token=token-placeholder", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body, contentType string
		code                    int
		transportError          error
		want                    string
	}{
		{name: "ok", code: 200, body: "rules: ['MATCH,DIRECT']", contentType: "text/plain"},
		{name: "YAML incorrectly labeled HTML", code: 200, body: "proxies: []", contentType: "text/html; charset=utf-8"},
		{name: "AGE incorrectly labeled HTML", code: 200, body: "-----BEGIN AGE ENCRYPTED FILE-----\nfixture", contentType: "text/html"},
		{name: "html", code: 200, body: "<html>placeholder</html>", contentType: "text/html; charset=utf-8", want: "网页"},
		{name: "HTML mislabeled as text", code: 200, body: "\ufeff\n<!DOCTYPE html><html>login</html>", contentType: "text/plain", want: "User-Agent"},
		{name: "size", code: 200, body: strings.Repeat("Y", MaxYAMLBytes+1), want: "超过"},
		{name: "http status", code: 404, body: "token-placeholder", want: "404"},
		{name: "transport", transportError: errors.New("token-placeholder"), want: "无法连接"},
		{name: "fake IP error", transportError: errors.Join(errFakeIPResolution, errors.New("token-placeholder")), want: "检测到 Fake-IP"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if test.transportError != nil {
					return nil, test.transportError
				}
				return &http.Response{StatusCode: test.code, Body: io.NopCloser(strings.NewReader(test.body)),
					Header: http.Header{"Content-Type": {test.contentType}}, Request: r}, nil
			})}
			out, err := fetchYAML(client, request)
			if test.want == "" {
				if err != nil || out != test.body {
					t.Fatalf("valid YAML fetch failed: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "token-placeholder") {
				t.Fatalf("expected safe error containing %q", test.want)
			}
		})
	}
}

type fakeResolver []netip.Addr

func (f fakeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return f, nil
}

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
