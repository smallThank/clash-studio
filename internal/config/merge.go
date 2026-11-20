package config

import (
	"errors"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Conflict identifies a location that the draft and the refreshed source both changed.
type Conflict struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Merge previews the refreshed source alongside local edits. Unresolved conflicts retain the draft in the preview.
func Merge(base, draft, upstream string, resolutions map[string]string) (string, []Conflict, error) {
	for _, value := range resolutions {
		if value != "draft" && value != "upstream" {
			return "", nil, errors.New("冲突处理方式必须是 draft 或 upstream")
		}
	}
	documents := make([]*Document, 0, 3)
	for _, source := range []string{base, draft, upstream} {
		document, err := Parse(source)
		if err != nil {
			return "", nil, err
		}
		documents = append(documents, document)
	}
	state := mergeState{resolutions: resolutions, conflicts: []Conflict{}}
	result := Document{root: state.mergeNode("", documents[0].root, documents[1].root, documents[2].root)}
	encoded, err := result.YAML()
	return encoded, state.conflicts, err
}

type mergeState struct {
	resolutions map[string]string
	conflicts   []Conflict
}

func (state *mergeState) mergeNode(path string, base, draft, upstream *yaml.Node) *yaml.Node {
	switch {
	case sameValue(draft, upstream):
		return draft
	case sameValue(base, upstream):
		return draft
	case sameValue(base, draft):
		return reconcile(clone(draft), clone(upstream))
	case isMap(draft) && isMap(upstream) && (base == nil || isMap(base)):
		return state.mergeMap(path, base, draft, upstream)
	case path == "proxies" || path == "proxy-groups":
		if merged, ok := state.mergeNamedSequence(path, base, draft, upstream); ok {
			return merged
		}
	}
	if choice := state.resolutions[path]; choice == "draft" {
		return draft
	} else if choice == "upstream" {
		return upstream
	}
	state.conflicts = append(state.conflicts, Conflict{
		Path: path, Message: "草稿和上游订阅修改了同一项，请选择保留草稿或采用上游",
	})
	return draft
}

func sameValue(left, right *yaml.Node) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.Kind != right.Kind || left.ShortTag() != right.ShortTag() || len(left.Content) != len(right.Content) {
		return false
	}
	if left.Kind == yaml.ScalarNode {
		return equivalent(left, right)
	}
	if left.Kind == yaml.MappingNode {
		rightIndex := indexMapping(right)
		for i := 0; i < len(left.Content); i += 2 {
			if !sameValue(left.Content[i+1], rightIndex[left.Content[i].Value]) {
				return false
			}
		}
		return true
	}
	for i, child := range left.Content {
		if !sameValue(child, right.Content[i]) {
			return false
		}
	}
	return true
}

func isMap(node *yaml.Node) bool { return node != nil && node.Kind == yaml.MappingNode }

func (state *mergeState) mergeMap(path string, base, draft, upstream *yaml.Node) *yaml.Node {
	baseIndex, draftIndex, upstreamIndex := indexMapping(base), indexMapping(draft), indexMapping(upstream)
	seen := make(map[string]bool)
	merged := *draft
	merged.Content = nil
	for _, mapping := range []*yaml.Node{draft, upstream, base} {
		if !isMap(mapping) {
			continue
		}
		for i := 0; i < len(mapping.Content); i += 2 {
			key := mapping.Content[i]
			if seen[key.Value] {
				continue
			}
			seen[key.Value] = true
			at := mergePath(path, key.Value)
			value := state.mergeNode(at, baseIndex[key.Value], draftIndex[key.Value], upstreamIndex[key.Value])
			if value != nil {
				merged.Content = append(merged.Content, key, value)
			}
		}
	}
	return &merged
}

func mergePath(parent, field string) string {
	if parent == "" && field != "" && strings.IndexFunc(field, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) < 0 {
		return field
	}
	return childPath(parent, field)
}

func namedSequence(node *yaml.Node) ([]string, map[string]*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil, nil, false
	}
	names := make([]string, 0, len(node.Content))
	items := make(map[string]*yaml.Node, len(node.Content))
	for _, item := range node.Content {
		name, ok := stringValue(mappingValue(item, "name"))
		if !ok || name == "" || items[name] != nil {
			return nil, nil, false
		}
		names = append(names, name)
		items[name] = item
	}
	return names, items, true
}

func sharedOrder(names []string, other map[string]*yaml.Node) []string {
	var shared []string
	for _, name := range names {
		if other[name] != nil {
			shared = append(shared, name)
		}
	}
	return shared
}

func (state *mergeState) mergeNamedSequence(path string, base, draft, upstream *yaml.Node) (*yaml.Node, bool) {
	baseNames, baseValues, baseOK := namedSequence(base)
	draftNames, draftValues, draftOK := namedSequence(draft)
	upstreamNames, upstreamValues, upstreamOK := namedSequence(upstream)
	if !baseOK || !draftOK || !upstreamOK {
		return nil, false
	}

	// Compare the relative order of entries present in all three revisions.
	common := make(map[string]*yaml.Node)
	for _, name := range baseNames {
		if draftValues[name] != nil && upstreamValues[name] != nil {
			common[name] = baseValues[name]
		}
	}
	baseOrder := sharedOrder(baseNames, common)
	draftOrder := sharedOrder(draftNames, common)
	upstreamOrder := sharedOrder(upstreamNames, common)
	ordered, secondary := draftNames, upstreamNames
	draftReordered := !slices.Equal(baseOrder, draftOrder)
	upstreamReordered := !slices.Equal(baseOrder, upstreamOrder)
	if upstreamReordered && !draftReordered {
		ordered, secondary = upstreamNames, draftNames
	}
	if upstreamReordered && draftReordered && !slices.Equal(draftOrder, upstreamOrder) {
		orderPath := path + ".order"
		switch state.resolutions[orderPath] {
		case "upstream":
			ordered, secondary = upstreamNames, draftNames
		case "draft":
		default:
			state.conflicts = append(state.conflicts, Conflict{Path: orderPath, Message: "草稿和上游对条目进行了不同的排序"})
		}
	}

	order := combinedOrder(ordered, secondary)
	merged := *draft
	merged.Content = nil
	for _, name := range order {
		entryPath := childPath(path, name)
		value := state.mergeNode(entryPath, baseValues[name], draftValues[name], upstreamValues[name])
		if value != nil {
			merged.Content = append(merged.Content, value)
		}
	}
	return &merged, true
}

// Keep the primary list order, inserting secondary-only entries next to a shared predecessor.
func combinedOrder(primary, secondary []string) []string {
	inPrimary := make(map[string]bool, len(primary))
	for _, name := range primary {
		inPrimary[name] = true
	}
	extras := make(map[string][]string)
	anchor := ""
	for _, name := range secondary {
		if inPrimary[name] {
			anchor = name
			continue
		}
		extras[anchor] = append(extras[anchor], name)
	}
	out := make([]string, 0, len(primary)+len(secondary))
	out = append(out, extras[""]...)
	for _, name := range primary {
		out = append(out, name)
		out = append(out, extras[name]...)
	}
	return out
}
