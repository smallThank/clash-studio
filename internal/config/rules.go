package config

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

var simpleRules = strings.Fields(`
	DOMAIN DOMAIN-SUFFIX DOMAIN-KEYWORD DOMAIN-REGEX GEOSITE IP-CIDR IP-CIDR6 IP-SUFFIX IP-ASN
	SRC-IP-CIDR SRC-IP-CIDR6 SRC-IP-SUFFIX SRC-IP-ASN GEOIP SRC-GEOIP DST-PORT SRC-PORT
	IN-PORT IN-TYPE IN-USER IN-NAME PROCESS-PATH PROCESS-PATH-REGEX PROCESS-NAME PROCESS-NAME-REGEX
	UID NETWORK DSCP RULE-SET
`)

type rulePart struct {
	text  string
	start int
	end   int
}

func splitRule(raw string) ([]rulePart, error) {
	var parts []rulePart
	start, parentheses := 0, 0
	inClass, escaped := false, false
	appendPart := func(end int) {
		token := raw[start:end]
		text := strings.TrimSpace(token)
		offset := start + len(token) - len(strings.TrimLeftFunc(token, unicode.IsSpace))
		parts = append(parts, rulePart{text: text, start: offset, end: offset + len(text)})
	}
	for i, char := range raw {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '[' {
			inClass = true
			continue
		}
		if char == ']' && inClass {
			inClass = false
			continue
		}
		if inClass {
			continue
		}
		switch char {
		case '(':
			parentheses++
		case ')':
			parentheses--
			if parentheses < 0 {
				return nil, errors.New("规则的括号不匹配")
			}
		case ',':
			if parentheses == 0 {
				appendPart(i)
				start = i + 1
			}
		}
	}
	if parentheses != 0 || inClass {
		return nil, errors.New("规则的括号不匹配")
	}
	appendPart(len(raw))
	return parts, nil
}

func logicalRule(kind string) bool {
	return kind == "AND" || kind == "OR" || kind == "NOT"
}

func isKnownRule(kind string) bool {
	return kind == "MATCH" || kind == "SUB-RULE" || logicalRule(kind) || slices.Contains(simpleRules, kind)
}

func ruleArity(kind string, topLevel bool) int {
	if kind == "MATCH" {
		if topLevel {
			return 2
		}
		return 1
	}
	if topLevel || kind == "SUB-RULE" {
		return 3
	}
	return 2
}

func unwrapRule(token string) (string, bool) {
	if len(token) < 2 || token[0] != '(' || token[len(token)-1] != ')' {
		return "", false
	}
	return token[1 : len(token)-1], true
}

func walkLogical(kind, expression string, check func(string)) bool {
	inner, ok := unwrapRule(expression)
	if !ok {
		return false
	}
	if kind == "SUB-RULE" {
		// SUB-RULE accepts a single parenthesized rule. Some profiles use an extra parenthesis pair.
		if nested, wrapped := unwrapRule(inner); wrapped {
			inner = nested
		}
		check(inner)
		return true
	}
	parts, err := splitRule(inner)
	if err != nil || (kind == "NOT" && len(parts) != 1) || (kind != "NOT" && len(parts) < 2) {
		return false
	}
	for _, part := range parts {
		rule, ok := unwrapRule(part.text)
		if !ok {
			return false
		}
		check(rule)
	}
	return true
}

func eachRuleList(root *yaml.Node, visit func(name, path string, items []*yaml.Node)) {
	if list := sequence(mappingValue(root, "rules")); list != nil {
		visit("", "rules", list)
	}
	subs := mappingValue(root, "sub-rules")
	if subs == nil || subs.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(subs.Content); i += 2 {
		name := subs.Content[i].Value
		if list := sequence(subs.Content[i+1]); list != nil {
			visit(name, childPath("sub-rules", name), list)
		}
	}
}

func (v *validator) validateRules() {
	wasMatch := false
	subs := mappingValue(v.root, "sub-rules")
	knownSubs := make(map[string]bool)
	subgraph := make(graph)
	if subs != nil && subs.Kind == yaml.MappingNode {
		for i := 0; i < len(subs.Content); i += 2 {
			name, value := subs.Content[i].Value, subs.Content[i+1]
			path := childPath("sub-rules", name)
			knownSubs[name] = true
			if value.Kind != yaml.SequenceNode {
				v.error(path, "子规则需要列表")
			}
			subgraph[name] = &vertex{path: path}
		}
	}
	eachRuleList(v.root, func(name, path string, items []*yaml.Node) {
		wasMatch = false
		for index, item := range items {
			at := itemPath(path, index)
			raw, ok := stringValue(item)
			if !ok || strings.TrimSpace(raw) == "" {
				v.error(at, "规则必须为非空字符串")
				continue
			}
			if wasMatch {
				v.error(at, "此规则位于 MATCH 之后，无法匹配")
			}
			kind := v.validateRule(at, name, raw, true, 0, knownSubs, subgraph)
			wasMatch = wasMatch || kind == "MATCH"
		}
	})
	v.reportCycles(subgraph, "子规则形成循环引用")
}

func (v *validator) validateRule(path, subName, raw string, topLevel bool, depth int, subs map[string]bool, subgraph graph) string {
	if depth >= maxYAMLDepth {
		v.error(path, "规则嵌套过深")
		return ""
	}
	parts, err := splitRule(raw)
	if err != nil {
		v.error(path, err.Error())
		return ""
	}
	kind := strings.ToUpper(parts[0].text)
	if !isKnownRule(kind) {
		v.warn(path, "保留此规则类型，请用目标客户端验证")
		return kind
	}
	required := ruleArity(kind, topLevel)
	if len(parts) < required {
		v.error(path, "规则缺少必需的匹配内容或目标策略")
		return kind
	}
	for _, part := range parts[:required] {
		if part.text == "" {
			v.error(path, "规则含空的必填项")
			return kind
		}
	}
	if kind == "MATCH" && len(parts) != required {
		v.error(path, "MATCH 规则含多余字段")
	}
	if topLevel && kind != "SUB-RULE" {
		v.policyRef(path, parts[required-1].text)
	}
	if kind == "SUB-RULE" {
		target := parts[2].text
		if !subs[target] {
			v.error(path, "SUB-RULE 引用了不存在的子规则: "+target)
		} else if subName != "" {
			subgraph[subName].edges = append(subgraph[subName].edges, target)
		}
	}
	if kind == "RULE-SET" && !v.ruleSets[parts[1].text] {
		v.error(path, "规则集不存在: "+parts[1].text)
	}
	switch kind {
	case "DOMAIN-REGEX", "PROCESS-NAME-REGEX", "PROCESS-PATH-REGEX":
		v.regex(path, parts[1].text)
	case "IP-CIDR", "IP-CIDR6", "SRC-IP-CIDR", "SRC-IP-CIDR6":
		if addr, err := netip.ParsePrefix(parts[1].text); err != nil || (strings.HasSuffix(kind, "6") && !addr.Addr().Is6()) {
			v.error(path, "需要有效的 CIDR 网段")
		}
	}
	if logicalRule(kind) || kind == "SUB-RULE" {
		valid := walkLogical(kind, parts[1].text, func(rule string) {
			v.validateRule(path, subName, rule, false, depth+1, subs, subgraph)
		})
		if !valid {
			v.error(path, fmt.Sprintf("%s 的嵌套规则格式无效", kind))
		}
	}
	for _, option := range parts[required:] {
		switch option.text {
		case "src", "dst", "no-resolve":
		case "":
			v.error(path, "规则含空的附加选项")
		default:
			v.warn(path, "附加选项需用目标客户端验证")
		}
	}
	return kind
}
