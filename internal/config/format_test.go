package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewFieldsUseYAMLQuoting(t *testing.T) {
	doc, err := Parse("mode: 'Rule'\n")
	if err != nil {
		t.Fatal(err)
	}
	changes := map[string]json.RawMessage{
		"proxies": json.RawMessage(`[{"name":"出口","dialer-proxy":"中继","password":"true"}]`),
	}
	if err := doc.Apply(changes, nil, nil); err != nil {
		t.Fatal(err)
	}
	yaml, err := doc.YAML()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mode: 'Rule'", "dialer-proxy: 中继", `password: "true"`} {
		if !strings.Contains(yaml, want) {
			t.Errorf("generated YAML is missing %q:\n%s", want, yaml)
		}
	}
}
