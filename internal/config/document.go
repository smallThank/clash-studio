package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	MaxYAMLBytes = 2 << 20
	maxYAMLDepth = 80
	maxYAMLNodes = 125_000
	maxAliases   = 128
)

const DefaultYAML = "mixed-port: 7890\nmode: rule\nrules:\n  - MATCH,DIRECT\n"

// Document keeps the YAML AST so edits can retain untouched values, comments, and ordering.
type Document struct {
	root *yaml.Node
}

func Parse(source string) (*Document, error) {
	if len(source) > MaxYAMLBytes {
		return nil, errors.New("YAML 超出 2 MiB 限制")
	}

	decoder := yaml.NewDecoder(strings.NewReader(source))
	var node yaml.Node
	if err := decoder.Decode(&node); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("请输入 YAML 配置")
		}
		return nil, fmt.Errorf("YAML 解析失败: %w", err)
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("YAML 根节点必须是键值映射")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("只支持单个 YAML 文档")
	}

	expander := expansion{visiting: make(map[*yaml.Node]bool)}
	root, err := expander.expand(node.Content[0], 0)
	if err != nil {
		return nil, err
	}
	return &Document{root: root}, nil
}

func (doc *Document) YAML() (string, error) {
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc.root); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	if output.Len() > MaxYAMLBytes {
		return "", errors.New("生成的 YAML 超出 2 MiB 限制")
	}
	return output.String(), nil
}

type expansion struct {
	visiting map[*yaml.Node]bool
	nodes    int
	aliases  int
}

func (e *expansion) expand(node *yaml.Node, depth int) (*yaml.Node, error) {
	e.nodes++
	if depth > maxYAMLDepth || e.nodes > maxYAMLNodes {
		return nil, errors.New("YAML 嵌套或展开后的规模超过限制")
	}
	if node.Kind == yaml.AliasNode {
		e.aliases++
		if node.Alias == nil || e.visiting[node.Alias] || e.aliases > maxAliases {
			return nil, errors.New("YAML 含循环引用或过多别名")
		}
		target, err := e.expand(node.Alias, depth+1)
		if err != nil {
			return nil, err
		}
		if node.LineComment != "" {
			target.LineComment = node.LineComment
		}
		if node.HeadComment != "" {
			target.HeadComment = node.HeadComment
		}
		if node.FootComment != "" {
			target.FootComment = node.FootComment
		}
		return target, nil
	}
	e.visiting[node] = true
	defer delete(e.visiting, node)
	switch node.ShortTag() {
	case "!!map", "!!seq", "!!str", "!!bool", "!!int", "!!float", "!!null", "!!timestamp", "!!binary", "!!merge":
	default:
		return nil, fmt.Errorf("第 %d 行的 YAML 标签未受支持", node.Line)
	}
	result := *node
	result.Anchor = ""
	result.Content = nil
	if node.Kind != yaml.MappingNode {
		for _, child := range node.Content {
			copied, err := e.expand(child, depth+1)
			if err != nil {
				return nil, err
			}
			result.Content = append(result.Content, copied)
		}
		return &result, nil
	}
	return e.expandMapping(node, &result, depth)
}

func (e *expansion) expandMapping(source, result *yaml.Node, depth int) (*yaml.Node, error) {
	keys := make(map[string]bool, len(source.Content)/2)
	for i := 0; i < len(source.Content); i += 2 {
		key := source.Content[i]
		if key.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("第 %d 行的 YAML 键必须是标量", key.Line)
		}
		if keys[key.Value] {
			return nil, fmt.Errorf("第 %d 行含重复的 YAML 键", key.Line)
		}
		keys[key.Value] = true
	}
	for i := 0; i < len(source.Content); i += 2 {
		key, value := source.Content[i], source.Content[i+1]
		copied, err := e.expand(value, depth+1)
		if err != nil {
			return nil, err
		}
		if key.ShortTag() == "!!merge" {
			sources := []*yaml.Node{copied}
			if copied.Kind == yaml.SequenceNode {
				sources = copied.Content
			}
			for _, candidate := range sources {
				if candidate.Kind != yaml.MappingNode {
					return nil, fmt.Errorf("第 %d 行的 YAML 合并键需要映射", key.Line)
				}
				for j := 0; j < len(candidate.Content); j += 2 {
					mergedKey := candidate.Content[j]
					if !keys[mergedKey.Value] {
						result.Content = append(result.Content, mergedKey, candidate.Content[j+1])
						keys[mergedKey.Value] = true
					}
				}
			}
			continue
		}
		copiedKey, err := e.expand(key, depth+1)
		if err != nil {
			return nil, err
		}
		result.Content = append(result.Content, copiedKey, copied)
	}
	return result, nil
}

type Rename struct {
	Kind string `json:"kind"`
	From string `json:"from"`
	To   string `json:"to"`
}

func (doc *Document) Apply(changes map[string]json.RawMessage, remove []string, rename *Rename) error {
	working := clone(doc.root)
	var normalized Rename
	if rename != nil {
		var err error
		normalized, err = normalizeRename(*rename)
		if err != nil {
			return err
		}
		if err := renameEntity(working, normalized); err != nil {
			return err
		}
	}
	for _, key := range slices.Sorted(maps.Keys(changes)) {
		existing := mappingValue(working, key)
		var incoming yaml.Node
		if !json.Valid(changes[key]) {
			return fmt.Errorf("字段 %s 需要有效的 JSON 值", key)
		}
		if err := yaml.Unmarshal(changes[key], &incoming); err != nil || len(incoming.Content) != 1 {
			return fmt.Errorf("字段 %s 无法转换为 YAML", key)
		}
		value := incoming.Content[0]
		normalizeStyles(value)
		setMappingValue(working, key, reconcile(existing, value))
	}
	for _, key := range remove {
		setMappingValue(working, key, nil)
	}

	if rename != nil {
		if err := renameReferences(working, normalized); err != nil {
			return err
		}
	}
	return doc.replaceRoot(working)
}

func (doc *Document) replaceRoot(working *yaml.Node) error {
	updated := &Document{root: working}
	encoded, err := updated.YAML()
	if err != nil {
		return err
	}
	parsed, err := Parse(encoded)
	if err != nil {
		return err
	}
	doc.root = parsed.root
	return nil
}

// JSON maps and lists should inherit existing formatting or use ordinary YAML block style.
func normalizeStyles(node *yaml.Node) {
	if node.Kind == yaml.MappingNode || node.Kind == yaml.SequenceNode {
		node.Style = 0
	}
	if node.Kind == yaml.ScalarNode {
		node.Style &^= yaml.DoubleQuotedStyle
	}
	for _, child := range node.Content {
		normalizeStyles(child)
	}
}

func reconcile(existing, incoming *yaml.Node) *yaml.Node {
	if existing == nil || incoming == nil {
		return incoming
	}
	if equivalent(existing, incoming) {
		return existing
	}
	incoming.HeadComment = existing.HeadComment
	incoming.LineComment = existing.LineComment
	incoming.FootComment = existing.FootComment
	if existing.Kind != incoming.Kind {
		return incoming
	}
	switch incoming.Kind {
	case yaml.MappingNode:
		incoming.Style = existing.Style
		old := indexMapping(existing)
		new := indexMapping(incoming)
		items := make([]*yaml.Node, 0, len(incoming.Content))
		for i := 0; i < len(existing.Content); i += 2 {
			key := existing.Content[i]
			if value, exists := new[key.Value]; exists {
				items = append(items, key, reconcile(existing.Content[i+1], value))
			}
		}
		for i := 0; i < len(incoming.Content); i += 2 {
			key := incoming.Content[i]
			if _, exists := old[key.Value]; !exists {
				items = append(items, key, incoming.Content[i+1])
			}
		}
		incoming.Content = items
	case yaml.SequenceNode:
		incoming.Style = existing.Style
		reconcileSequence(existing, incoming)
	case yaml.ScalarNode:
		if incoming.Tag == existing.Tag {
			incoming.Style = existing.Style
		}
	}
	return incoming
}

// Sequence entries follow their names or exact scalar text across rearrangements.
func reconcileSequence(existing, incoming *yaml.Node) {
	indices := make(map[string][]int)
	matchedOld := make([]bool, len(existing.Content))
	matchedNew := make([]bool, len(incoming.Content))
	for i, item := range existing.Content {
		if id := entryID(item); id != "" {
			indices[id] = append(indices[id], i)
		}
	}
	for i, item := range incoming.Content {
		id := entryID(item)
		candidates := indices[id]
		if id == "" || len(candidates) == 0 {
			continue
		}
		old := candidates[0]
		matchedOld[old], matchedNew[i] = true, true
		indices[id] = candidates[1:]
		incoming.Content[i] = reconcile(existing.Content[old], item)
	}
	if len(existing.Content) != len(incoming.Content) {
		return
	}
	for i, item := range incoming.Content {
		if !matchedOld[i] && !matchedNew[i] && existing.Content[i].Kind == item.Kind {
			incoming.Content[i] = reconcile(existing.Content[i], item)
		}
	}
}

func entryID(node *yaml.Node) string {
	if name, ok := stringValue(mappingValue(node, "name")); ok {
		return "name:" + name
	}
	if node.Kind == yaml.ScalarNode {
		return node.ShortTag() + ":" + node.Value
	}
	return ""
}

func equivalent(left, right *yaml.Node) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.Kind != right.Kind || left.ShortTag() != right.ShortTag() || len(left.Content) != len(right.Content) {
		return false
	}
	if left.Kind == yaml.ScalarNode {
		if left.ShortTag() == "!!str" {
			return left.Value == right.Value
		}
		var a, b any
		if left.Decode(&a) == nil && right.Decode(&b) == nil {
			if x, ok := a.(float64); ok && math.IsNaN(x) {
				y, ok := b.(float64)
				return ok && math.IsNaN(y)
			}
			return a == b
		}
		return left.Value == right.Value
	}
	for i := range left.Content {
		if !equivalent(left.Content[i], right.Content[i]) {
			return false
		}
	}
	return true
}

func clone(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	result := *node
	result.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		result.Content[i] = clone(child)
	}
	return &result
}

func indexMapping(node *yaml.Node) map[string]*yaml.Node {
	result := make(map[string]*yaml.Node)
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			result[node.Content[i].Value] = node.Content[i+1]
		}
	}
	return result
}

func mappingIndex(node *yaml.Node, field string) int {
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == field {
				return i
			}
		}
	}
	return -1
}

func mappingValue(node *yaml.Node, field string) *yaml.Node {
	if i := mappingIndex(node, field); i >= 0 {
		return node.Content[i+1]
	}
	return nil
}

func setMappingValue(node *yaml.Node, field string, value *yaml.Node) {
	i := mappingIndex(node, field)
	switch {
	case i < 0 && value != nil:
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: field}, value)
	case i >= 0 && value == nil:
		node.Content = slices.Delete(node.Content, i, i+2)
	case i >= 0:
		node.Content[i+1] = value
	}
}

func stringValue(node *yaml.Node) (string, bool) {
	if node != nil && node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str" {
		return node.Value, true
	}
	return "", false
}

func sequence(node *yaml.Node) []*yaml.Node {
	if node != nil && node.Kind == yaml.SequenceNode {
		return node.Content
	}
	return nil
}

func hasSequenceString(node *yaml.Node, name string) bool {
	return slices.ContainsFunc(sequence(node), func(item *yaml.Node) bool {
		value, ok := stringValue(item)
		return ok && value == name
	})
}
