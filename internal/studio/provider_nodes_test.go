package studio

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"testing"
)

func TestProviderNodeLookupUsesMihomoUserAgentByDefault(t *testing.T) {
	handler, _ := testOpen(t, filepath.Join(t.TempDir(), "studio.db"), Options{
		Fetch: func(_ context.Context, _, agent string) (string, error) {
			if agent == "clash.meta/1.19.34" {
				return "proxies: [{name: 'Node', type: socks5}]", nil
			}
			return "message: unsupported client", nil
		},
	})
	client := registerClient(t, handler, "DefaultProviderAgent")
	const url = "https://subscription.example.invalid/nodes"
	rec := client.request(handler, http.MethodPost, apiPrefix+"/imports/provider-nodes",
		providerNodesRequest{URL: url, Refresh: true}, nil)
	expect(t, rec, http.StatusOK)
	if names := decode[providerNodesResponse](t, rec).Nodes; !slices.Equal(names, []string{"Node"}) {
		t.Fatalf("default User-Agent did not get a node list: %v", names)
	}
	expect(t, client.request(handler, http.MethodPost, apiPrefix+"/imports/probe", probeRequest{Mode: "provider", URL: url}, nil), http.StatusOK)
}

func TestProviderNodeCachePersistsAndRefreshesExplicitly(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "studio.db")
	source := providerNodesRequest{URL: "https://subscription.example.invalid/nodes?token=private", UserAgent: "Mihomo-Test"}
	remote := "proxies: [{name: '🇭🇰 Private node', type: socks5}, {name: '🇭🇰 Private node', type: socks5}]"
	fail := false
	fetches := 0
	opts := Options{Fetch: func(_ context.Context, url, agent string) (string, error) {
		fetches++
		if url != source.URL || agent != source.UserAgent {
			t.Fatalf("unexpected subscription address or User-Agent")
		}
		if fail {
			return "", errors.New("upstream unavailable")
		}
		return remote, nil
	}}
	handler, db := testOpen(t, filename, opts)
	alice := registerClient(t, handler, "CacheOwner")
	bob := registerClient(t, handler, "CacheOther")
	const path = apiPrefix + "/imports/provider-nodes"
	expect(t, (testClient{}).request(handler, http.MethodPost, path, source, nil), http.StatusUnauthorized)
	read := func(client testClient, request providerNodesRequest) providerNodesResponse {
		t.Helper()
		rec := client.request(handler, http.MethodPost, path, request, nil)
		expect(t, rec, http.StatusOK)
		return decode[providerNodesResponse](t, rec)
	}
	if got := read(alice, source); got.Nodes != nil || got.FetchedAt != nil || fetches != 0 {
		t.Fatalf("cache miss made an upstream request or returned data: %+v", got)
	}
	refresh := source
	refresh.Refresh = true
	cached := read(alice, refresh)
	if !slices.Equal(cached.Nodes, []string{"🇭🇰 Private node"}) || cached.FetchedAt == nil || fetches != 1 {
		t.Fatalf("refresh did not return the cached node names: %+v", cached)
	}
	otherAgent := source
	otherAgent.UserAgent = "Another Client"
	for _, result := range []providerNodesResponse{read(bob, source), read(alice, otherAgent)} {
		if result.Nodes != nil || fetches != 1 {
			t.Fatal("cache leaked across accounts or User-Agents")
		}
	}

	var sealed []byte
	if err := db.QueryRow("SELECT sealed_nodes FROM provider_node_cache").Scan(&sealed); err != nil || bytes.Contains(sealed, []byte("Private node")) {
		t.Fatalf("node names should be encrypted on disk: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	handler, _ = testOpen(t, filename, opts)
	fail = true
	expect(t, alice.request(handler, http.MethodPost, path, refresh, nil), http.StatusUnprocessableEntity)
	if got := read(alice, source); !slices.Equal(got.Nodes, cached.Nodes) || got.FetchedAt == nil ||
		!got.FetchedAt.Equal(*cached.FetchedAt) || fetches != 2 {
		t.Fatalf("restart or failed refresh lost the previous cache: %+v", got)
	}
	fail = false
	remote = "proxies: [{name: 'New node', type: socks5}]"
	read(alice, refresh)
	if got := read(alice, source); !slices.Equal(got.Nodes, []string{"New node"}) || fetches != 3 {
		t.Fatalf("successful refresh did not replace the cached list: %+v", got)
	}
}
