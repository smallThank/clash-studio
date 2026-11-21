package config

import (
	"errors"

	"gopkg.in/yaml.v3"
)

type NodeImportResult struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

// AppendProxyNodes retains existing policies and adds the first available node for each name.
func (doc *Document) AppendProxyNodes(source string) (NodeImportResult, error) {
	var result NodeImportResult
	incoming, err := ParseImport(source, "nodes")
	if err != nil {
		return result, err
	}
	existing := mappingValue(doc.root, "proxies")
	if existing != nil && existing.Kind != yaml.SequenceNode {
		return result, errors.New("当前配置的 proxies 需要是列表，请先修复此字段")
	}

	used := make(map[string]bool)
	for _, name := range builtins {
		used[name] = true
	}
	for _, key := range []string{"proxies", "proxy-groups"} {
		for _, item := range sequence(mappingValue(doc.root, key)) {
			name, _ := stringValue(mappingValue(item, "name"))
			used[name] = true
		}
	}
	var added []*yaml.Node
	for _, node := range sequence(mappingValue(incoming.root, "proxies")) {
		name, _ := stringValue(mappingValue(node, "name"))
		if used[name] {
			result.Skipped++
			continue
		}
		used[name] = true
		added = append(added, node)
	}

	result.Added = len(added)
	if result.Added == 0 {
		return result, nil
	}
	working := clone(doc.root)
	proxies := mappingValue(working, "proxies")
	if proxies == nil {
		proxies = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setMappingValue(working, "proxies", proxies)
	}
	proxies.Content = append(proxies.Content, added...)
	return result, doc.replaceRoot(working)
}
