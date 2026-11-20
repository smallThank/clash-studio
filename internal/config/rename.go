package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

func normalizeRename(rename Rename) (Rename, error) {
	switch rename.Kind {
	case "proxy", "node", "proxies":
		rename.Kind = "proxies"
	case "group", "proxy-group", "proxy-groups":
		rename.Kind = "proxy-groups"
	case "provider", "proxy-provider", "proxy-providers":
		rename.Kind = "proxy-providers"
	case "rule-set", "rule-provider", "rule-providers":
		rename.Kind = "rule-providers"
	default:
		return rename, errors.New("此配置模块不支持名称替换")
	}
	for _, name := range []string{rename.From, rename.To} {
		if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, ",\r\n") {
			return rename, errors.New("名称不能为空，也不能含逗号、换行或首尾空白")
		}
	}
	if renamesPolicy(rename) && slices.Contains(builtins, rename.To) {
		return rename, errors.New("新名称属于内置策略")
	}
	return rename, nil
}

func renamesPolicy(rename Rename) bool {
	return rename.Kind == "proxies" || rename.Kind == "proxy-groups"
}

func findDefinition(root *yaml.Node, kind, name string) *yaml.Node {
	section := mappingValue(root, kind)
	if section == nil {
		return nil
	}
	if kind == "proxy-providers" || kind == "rule-providers" {
		if index := mappingIndex(section, name); index >= 0 {
			return section.Content[index]
		}
		return nil
	}
	for _, item := range sequence(section) {
		node := mappingValue(item, "name")
		if value, ok := stringValue(node); ok && value == name {
			return node
		}
	}
	return nil
}

func renameEntity(root *yaml.Node, rename Rename) error {
	from := findDefinition(root, rename.Kind, rename.From)
	if from == nil {
		return errors.New("原名称不存在，无法重新命名")
	}
	to := findDefinition(root, rename.Kind, rename.To)
	if renamesPolicy(rename) {
		other := "proxies"
		if rename.Kind == other {
			other = "proxy-groups"
		}
		if findDefinition(root, other, rename.To) != nil {
			return errors.New("新名称已被节点或代理组使用")
		}
	}
	if to != nil && from != to {
		return errors.New("新名称已存在")
	}
	from.Value = rename.To
	return nil
}

func renameReferences(root *yaml.Node, rename Rename) error {
	if rename.From == rename.To {
		return nil
	}
	if findDefinition(root, rename.Kind, rename.To) == nil || findDefinition(root, rename.Kind, rename.From) != nil {
		return errors.New("配置变更与重命名不一致")
	}

	if renamesPolicy(rename) {
		for _, node := range sequence(mappingValue(root, "proxies")) {
			renameValue(mappingValue(node, "dialer-proxy"), rename)
		}
		for _, group := range sequence(mappingValue(root, "proxy-groups")) {
			renameSequence(mappingValue(group, "proxies"), rename)
		}
		for _, key := range []string{"proxy-providers", "rule-providers"} {
			providers := mappingValue(root, key)
			if providers == nil || providers.Kind != yaml.MappingNode {
				continue
			}
			for i := 1; i < len(providers.Content); i += 2 {
				provider := providers.Content[i]
				renameValue(mappingValue(provider, "proxy"), rename)
				if key == "proxy-providers" {
					renameValue(mappingValue(mappingValue(provider, "override"), "dialer-proxy"), rename)
					for _, node := range sequence(mappingValue(provider, "payload")) {
						renameValue(mappingValue(node, "dialer-proxy"), rename)
					}
				}
			}
		}
	}
	if rename.Kind == "proxy-providers" {
		for _, group := range sequence(mappingValue(root, "proxy-groups")) {
			renameSequence(mappingValue(group, "use"), rename)
		}
	}
	if renamesPolicy(rename) || rename.Kind == "rule-providers" {
		eachRuleList(root, func(_, _ string, rules []*yaml.Node) {
			for _, node := range rules {
				if raw, ok := stringValue(node); ok {
					node.Value = renameRuleReference(raw, rename, true, 0)
				}
			}
		})
	}
	if rename.Kind == "rule-providers" {
		return renameDNSRuleSets(mappingValue(root, "dns"), rename)
	}
	return nil
}

func renameValue(node *yaml.Node, rename Rename) {
	if name, ok := stringValue(node); ok && name == rename.From {
		node.Value = rename.To
	}
}

func renameSequence(node *yaml.Node, rename Rename) {
	for _, item := range sequence(node) {
		renameValue(item, rename)
	}
}

type ruleEdit struct {
	start int
	end   int
	text  string
}

func renameRuleReference(raw string, rename Rename, topLevel bool, depth int) string {
	if depth >= maxYAMLDepth {
		return raw
	}
	parts, err := splitRule(raw)
	if err != nil {
		return raw
	}
	kind := strings.ToUpper(parts[0].text)
	required := ruleArity(kind, topLevel)
	if !isKnownRule(kind) || len(parts) < required {
		return raw
	}
	var edits []ruleEdit
	if topLevel && kind != "SUB-RULE" && renamesPolicy(rename) {
		part := parts[required-1]
		if part.text == rename.From {
			edits = append(edits, ruleEdit{part.start, part.end, rename.To})
		}
	}
	if kind == "RULE-SET" && rename.Kind == "rule-providers" && parts[1].text == rename.From {
		part := parts[1]
		edits = append(edits, ruleEdit{part.start, part.end, rename.To})
	}
	if rename.Kind == "rule-providers" && (logicalRule(kind) || kind == "SUB-RULE") {
		part := parts[1]
		if updated := renameNestedReferences(kind, part.text, rename, depth+1); updated != part.text {
			edits = append(edits, ruleEdit{part.start, part.end, updated})
		}
	}
	return editRuleText(raw, edits)
}

func editRuleText(raw string, edits []ruleEdit) string {
	slices.SortFunc(edits, func(a, b ruleEdit) int { return b.start - a.start })
	for _, edit := range edits {
		raw = raw[:edit.start] + edit.text + raw[edit.end:]
	}
	return raw
}

func renameNestedReferences(kind, raw string, rename Rename, depth int) string {
	inner, ok := unwrapRule(raw)
	if !ok {
		return raw
	}
	if kind == "SUB-RULE" {
		if nested, unwrapped := unwrapRule(inner); unwrapped {
			return "((" + renameRuleReference(nested, rename, false, depth) + "))"
		}
		return "(" + renameRuleReference(inner, rename, false, depth) + ")"
	}
	parts, err := splitRule(inner)
	if err != nil {
		return raw
	}
	var edits []ruleEdit
	for _, part := range parts {
		if nested, ok := unwrapRule(part.text); ok {
			updated := renameRuleReference(nested, rename, false, depth)
			if updated != nested {
				edits = append(edits, ruleEdit{part.start + 1, part.end - 1, updated})
			}
		}
	}
	return "(" + editRuleText(inner, edits) + ")"
}

func renameDNSRuleSets(dns *yaml.Node, rename Rename) error {
	for _, field := range []string{"nameserver-policy", "proxy-server-nameserver-policy"} {
		policy := mappingValue(dns, field)
		if policy == nil || policy.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i < len(policy.Content); i += 2 {
			item := policy.Content[i]
			items, ok := strings.CutPrefix(item.Value, "rule-set:")
			if !ok {
				continue
			}
			tokens := strings.Split(items, ",")
			for j, token := range tokens {
				if strings.TrimSpace(token) == rename.From {
					tokens[j] = strings.Replace(token, rename.From, rename.To, 1)
				}
			}
			replacement := "rule-set:" + strings.Join(tokens, ",")
			if other := mappingIndex(policy, replacement); other >= 0 && other != i {
				return fmt.Errorf("重命名会使 dns.%s 出现重复入口", field)
			}
			item.Value = replacement
		}
	}
	for _, item := range sequence(mappingValue(dns, "fake-ip-filter")) {
		if raw, ok := stringValue(item); ok {
			item.Value = renameRuleReference(raw, rename, true, 0)
		}
	}
	return nil
}
