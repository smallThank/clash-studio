package config

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
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
	if _, err := publicAddresses(ctx, resolver, "example.invalid"); !errors.Is(err, errNonPublic) {
		t.Fatalf("mixed DNS response should be blocked before dialing: %v", err)
	}
	resolver = fakeResolver([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2606:4700::1111")})
	addrs, err := publicAddresses(ctx, resolver, "example.invalid")
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
		{name: "html", code: 200, body: "<html>placeholder</html>", contentType: "text/html; charset=utf-8", want: "网页"},
		{name: "size", code: 200, body: strings.Repeat("Y", MaxYAMLBytes+1), want: "超过"},
		{name: "http status", code: 404, body: "token-placeholder", want: "404"},
		{name: "transport", transportError: errors.New("token-placeholder"), want: "无法连接"},
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
