package config

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestYAMLRoundTripAndPatchAtSampleScale(t *testing.T) {
	var fixture strings.Builder
	fixture.WriteString(`# Portable configuration
mode: Rule
mixed-port: 0
allow-lan: false
dns:
  enable: true # preserve DNS comment
  nameserver: [https://dns.example.invalid/query]
  nameserver-policy:
    "example.invalid": https://dns.example.invalid/query
    "+.example.invalid": [https://dns.example.invalid/query]
  fake-ip-filter:
`)
	for i := 0; i < 95; i++ {
		fmt.Fprintf(&fixture, "    - +.host-%d.example.invalid\n", i)
	}
	fixture.WriteString(`proxy-providers:
  remote:
    type: http
    url: https://subscriptions.example.invalid/nodes.yaml
    health-check: {enable: false, interval: 300, url: https://probe.example.invalid/}
proxies:
  - {name: First, type: socks5, server: first.example.invalid, port: 1080}
  - {name: Last, type: socks5, server: last.example.invalid, port: 1080, dialer-proxy: First}
proxy-groups:
`)
	for i := 0; i < 73; i++ {
		kind := "select"
		if i < 11 {
			kind = "url-test"
		} else if i < 15 {
			kind = "fallback"
		}
		fmt.Fprintf(&fixture, "  - {name: Group%d, type: %s, proxies: [Last, DIRECT], use: [remote]}\n", i, kind)
	}
	fixture.WriteString("rule-providers:\n")
	for i := 0; i < 59; i++ {
		fmt.Fprintf(&fixture, "  Set%d: {type: http, behavior: classical, url: https://rules.example.invalid/%d, path: ./rules/%d.yaml, proxy: Group0}\n", i, i, i)
	}
	fixture.WriteString("rules:\n")
	for i := 0; i < 59; i++ {
		fmt.Fprintf(&fixture, "  - RULE-SET,Set%d,Group%d,no-resolve\n", i, i)
	}
	for i := 0; i < 14; i++ {
		fmt.Fprintf(&fixture, "  - DOMAIN,rule%d.example.invalid,Group0\n", i)
	}
	fixture.WriteString("  - MATCH,DIRECT\nx-extra:\n  preserve: null\n  enable: false\n  listeners: []\n")

	original := mustParse(t, fixture.String())
	if issues := original.Validate(); len(issues) != 0 {
		t.Fatalf("valid fixture raised issues: %+v", issues)
	}
	before := indexMapping(original.root)
	encoded := mustYAML(t, original)
	if !sameValue(original.root, mustParse(t, encoded).root) {
		t.Fatal("serialization changed sample semantics")
	}
	dns := mappingValue(original.root, "dns")
	dnsJSON := map[string]any{}
	if err := dns.Decode(&dnsJSON); err != nil {
		t.Fatal(err)
	}
	dnsJSON["enable"] = false
	payload, err := json.Marshal(dnsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := original.Apply(map[string]json.RawMessage{"dns": payload}, nil, nil); err != nil {
		t.Fatal(err)
	}
	for key, value := range before {
		if key != "dns" && !sameValue(value, mappingValue(original.root, key)) {
			t.Errorf("DNS edit changed %s", key)
		}
	}
	output := mustYAML(t, original)
	if !strings.Contains(output, "# Portable configuration") || !strings.Contains(output, "enable: false # preserve DNS comment") {
		t.Fatalf("retaining comments after DNS edit failed")
	}
	for key, want := range map[string]int{"proxy-groups": 73, "rules": 74, "proxies": 2} {
		if got := len(sequence(mappingValue(original.root, key))); got != want {
			t.Errorf("%s count %d, want %d", key, got, want)
		}
	}
	if len(indexMapping(mappingValue(original.root, "rule-providers"))) != 59 {
		t.Fatal("rule provider count changed")
	}
}

func TestParseSafetyAndMerges(t *testing.T) {
	for name, source := range map[string]string{
		"duplicate":     "mode: rule\nmode: global\n",
		"custom tag":    "field: !custom value\n",
		"invalid key":   "!custom field: x\n",
		"multi":         "mode: rule\n---\nmode: global\n",
		"recursion":     "sample: &self {next: *self}\n",
		"root":          "- sequence\n",
		"invalid merge": "sample: {<<: [false]}\n",
		"too deep":      "sample: " + strings.Repeat("[", maxYAMLDepth+4) + "item" + strings.Repeat("]", maxYAMLDepth+4),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(source); err == nil {
				t.Fatal("expected invalid YAML rejection")
			}
		})
	}
	doc := mustParse(t, `first: &first {mode: rule, port: 0, allow-lan: false}
second: &second {mode: global, port: 8080}
merged: {<<: [*first, *second], port: 9090}
`)
	merged := mustYAML(t, doc)
	if !strings.Contains(merged, "merged: {mode: rule, allow-lan: false, port: 9090}") {
		t.Fatalf("unexpected merge precedence: %s", merged)
	}
	if strings.Contains(merged, "*first") || strings.Contains(merged, "&first") {
		t.Fatal("normalized output should contain expanded alias values")
	}
}

func TestRenameKeepsReferencesAndRuleFormatting(t *testing.T) {
	doc := mustParse(t, `
proxies:
  - {name: Tunnel, type: socks5, server: one.example.invalid, port: 1080}
  - {name: Endpoint, type: socks5, server: two.example.invalid, port: 1080, dialer-proxy: Tunnel}
proxy-providers:
  OldFeed:
    type: http
    url: https://provider.example.invalid
    override: {dialer-proxy: Tunnel}
proxy-groups:
  - {name: Policy, type: select, proxies: [Tunnel, Endpoint, DIRECT], use: [OldFeed]}
rule-providers:
  OldSet: {type: inline, behavior: domain, payload: [example.invalid], proxy: Tunnel}
dns:
  nameserver-policy:
    "rule-set:OldSet": dns.example.invalid
rules:
  - 'AND,((RULE-SET,  OldSet),(OR,((DOMAIN,example.invalid),(RULE-SET,OldSet)))), Tunnel'
  - SUB-RULE,(RULE-SET,OldSet),nested
  - MATCH,Policy
sub-rules:
  nested: ["MATCH,Tunnel"]
`)
	change := map[string]json.RawMessage{
		"proxies": json.RawMessage(`[{"name":"Upstream","type":"socks5","server":"one.example.invalid","port":1080},{"name":"Endpoint","type":"socks5","server":"two.example.invalid","port":1080,"dialer-proxy":"Tunnel"}]`),
	}
	for _, entry := range []struct {
		rename  Rename
		changes map[string]json.RawMessage
	}{
		{Rename{"proxies", "Tunnel", "Upstream"}, change},
		{Rename{"proxy-providers", "OldFeed", "Feed"}, nil},
		{Rename{"rule-providers", "OldSet", "Domains"}, nil},
	} {
		if err := doc.Apply(entry.changes, nil, &entry.rename); err != nil {
			t.Fatal(err)
		}
	}
	out := mustYAML(t, doc)
	for _, expected := range []string{
		"override: {dialer-proxy: Upstream}",
		"proxies: [Upstream, Endpoint, DIRECT]",
		"use: [Feed]",
		`"rule-set:Domains":`,
		"RULE-SET,  Domains",
		"SUB-RULE,(RULE-SET,Domains),nested",
		"MATCH,Upstream",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("renamed YAML missing %s", expected)
		}
	}
	if issues := doc.Validate(); len(issues) != 0 {
		t.Fatalf("renaming broke references: %+v", issues)
	}
	before := out
	if err := doc.Apply(nil, nil, &Rename{"proxies", "Upstream", "Policy"}); err == nil {
		t.Fatal("renaming to group name should fail")
	}
	if mustYAML(t, doc) != before {
		t.Fatal("failed edit mutated document")
	}
}

func TestRuleAndDependencyValidation(t *testing.T) {
	doc := mustParse(t, `
proxies:
  - {name: Node, type: socks5, server: node.example.invalid, port: 1080, dialer-proxy: Group}
proxy-groups:
  - {name: Group, type: fallback, proxies: [Node, DIRECT]}
rule-providers:
  R: {type: http, behavior: classical, format: mrs, url: https://rules.example.invalid}
rules:
  - AND,((RULE-SET,NoSuchSet),(DOMAIN,example.invalid)),NotFound
  - SUB-RULE,(NETWORK,tcp),A
  - MATCH,DIRECT
  - DOMAIN,example.invalid,DIRECT
sub-rules:
  A:
    - SUB-RULE,(NETWORK,tcp),B
  B:
    - SUB-RULE,(NETWORK,tcp),A
`)
	issues := doc.Validate()
	for _, expected := range []struct {
		path, fragment string
	}{
		{`rule-providers["R"].format`, "MRS"},
		{"rules[0]", "NoSuchSet"},
		{"rules[0]", "NotFound"},
		{"rules[3]", "MATCH"},
		{`sub-rules["A"]`, "循环"},
		{"proxy-groups[0]", "循环"},
	} {
		if !slices.ContainsFunc(issues, func(issue Issue) bool {
			return issue.Severity == "error" && issue.Path == expected.path && strings.Contains(issue.Message, expected.fragment)
		}) {
			t.Errorf("missing %s / %s in %+v", expected.path, expected.fragment, issues)
		}
	}
}

func mustParse(t *testing.T, raw string) *Document {
	t.Helper()
	doc, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func mustYAML(t *testing.T, doc *Document) string {
	t.Helper()
	out, err := doc.YAML()
	if err != nil {
		t.Fatal(err)
	}
	return out
}
