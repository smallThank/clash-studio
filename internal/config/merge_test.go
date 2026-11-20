package config

import (
	"slices"
	"strings"
	"testing"
)

func TestMergePreservesIndependentLocalAndUpstreamChanges(t *testing.T) {
	base := `
mode: rule
mixed-port: 7890
dns:
  enable: true
  nameserver: [dns.example.invalid]
proxy-groups:
  - {name: Policy, type: select, proxies: [DIRECT]}
  - {name: Other, type: fallback, proxies: [DIRECT]}
  - {name: Dropped, type: select, proxies: [DIRECT]}
`
	draft := `
mode: rule
mixed-port: 7890
dns:
  enable: false
  nameserver: [dns.example.invalid]
proxy-groups:
  - {name: Other, type: fallback, proxies: [DIRECT, REJECT]}
  - {name: Policy, type: select, proxies: [DIRECT]}
  - {name: Local, type: select, proxies: [REJECT]}
`
	upstream := `
mode: rule
mixed-port: 7891
dns:
  enable: true
  nameserver: [new-dns.example.invalid]
proxy-groups:
  - {name: Policy, type: select, proxies: [REJECT, DIRECT]}
  - {name: Other, type: fallback, proxies: [DIRECT]}
  - {name: Remote, type: select, proxies: [DIRECT]}
  - {name: Dropped, type: select, proxies: [DIRECT]}
`
	body, conflicts, err := Merge(base, draft, upstream, nil)
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("independent edits conflict: %v %v", conflicts, err)
	}
	got := mustParse(t, body)
	want := mustParse(t, `
mode: rule
mixed-port: 7891
dns:
  enable: false
  nameserver: [new-dns.example.invalid]
proxy-groups:
  - {name: Other, type: fallback, proxies: [DIRECT, REJECT]}
  - {name: Remote, type: select, proxies: [DIRECT]}
  - {name: Policy, type: select, proxies: [REJECT, DIRECT]}
  - {name: Local, type: select, proxies: [REJECT]}
`)
	if !sameValue(got.root, want.root) {
		t.Fatalf("merge did not preserve independent values:\n%s", body)
	}
}

func TestMergeConflictsAndResolutions(t *testing.T) {
	base := "mode: rule\nprofiles-extra: {a: 1, b: true}\nrules: ['MATCH,DIRECT']\n"
	draft := "mode: direct\nprofiles-extra: {a: 3, b: true}\nrules: ['DOMAIN,local.example.invalid,DIRECT', 'MATCH,DIRECT']\n"
	upstream := "mode: global\nprofiles-extra: {a: 5}\nrules: ['DOMAIN,remote.example.invalid,DIRECT', 'MATCH,DIRECT']\n"
	preview, conflicts, err := Merge(base, draft, upstream, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, len(conflicts))
	for i, conflict := range conflicts {
		paths[i] = conflict.Path
	}
	if !slices.Equal(paths, []string{"mode", `profiles-extra["a"]`, "rules"}) {
		t.Fatalf("unexpected conflicts: %v", conflicts)
	}
	if !strings.Contains(preview, "mode: direct") {
		t.Fatal("unresolved conflict should preview draft")
	}
	preview, conflicts, err = Merge(base, draft, upstream, map[string]string{
		"mode": "upstream", "rules": "draft", `profiles-extra["a"]`: "upstream",
	})
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("resolving conflicts failed: %v, %v", conflicts, err)
	}
	got := mustParse(t, preview)
	want := mustParse(t, "mode: global\nprofiles-extra: {a: 5}\nrules: ['DOMAIN,local.example.invalid,DIRECT', 'MATCH,DIRECT']\n")
	if !sameValue(got.root, want.root) {
		t.Fatalf("resolution selected incorrect values:\n%s", preview)
	}
}

func TestValidationAcceptsMihomoLookaroundAndRejectsBrokenRegex(t *testing.T) {
	doc := mustParse(t, `
mode: Rule
proxy-groups:
  - {name: First, type: select, proxies: [DIRECT], filter: '(?i)^(?!private).*region'}
  - {name: Second, type: select, proxies: [DIRECT], filter: '(broken'}
rules:
  - DOMAIN-REGEX,^(?!excluded).*example,DIRECT
  - MATCH,DIRECT
`)
	issues := doc.Validate()
	if len(issues) != 1 || issues[0].Path != "proxy-groups[1].filter" || issues[0].Severity != "error" {
		t.Fatalf("unexpected regexp checks: %+v", issues)
	}
	if !strings.Contains(mustYAML(t, doc), "mode: Rule") {
		t.Fatal("mode capitalization should survive validation")
	}
}
