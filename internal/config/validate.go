package config

import (
	"fmt"
	"maps"
	"net/netip"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
	"gopkg.in/yaml.v3"
)

type Issue struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

type validator struct {
	root              *yaml.Node
	issues            []Issue
	policies          map[string]bool
	providers         map[string]bool
	ruleSets          map[string]bool
	links             graph
	includesProviders map[string]bool
	regexSyntax       map[string]error
}

type vertex struct {
	path  string
	edges []string
}

type graph map[string]*vertex

func (doc *Document) Validate() []Issue {
	v := validator{
		root:              doc.root,
		issues:            []Issue{},
		policies:          make(map[string]bool),
		providers:         make(map[string]bool),
		ruleSets:          make(map[string]bool),
		links:             make(graph),
		includesProviders: make(map[string]bool),
		regexSyntax:       make(map[string]error),
	}
	for _, builtin := range builtins {
		v.policies[builtin] = true
	}
	v.checkSections()
	nodes := sequence(mappingValue(v.root, "proxies"))
	groups := sequence(mappingValue(v.root, "proxy-groups"))
	for _, section := range []struct {
		key   string
		items []*yaml.Node
	}{{"proxies", nodes}, {"proxy-groups", groups}} {
		for i, item := range section.items {
			at := itemPath(section.key, i)
			if item.Kind != yaml.MappingNode {
				v.error(at, "条目需要键值映射")
				continue
			}
			name := v.requiredName(at+".name", mappingValue(item, "name"))
			if name != "" {
				if v.policies[name] {
					v.error(at+".name", "名称已被使用或属于内置策略")
				} else {
					v.policies[name] = true
					v.links["policy:"+name] = &vertex{path: at}
				}
			}
		}
	}
	v.collectProviders("proxy-providers", v.providers)
	v.collectProviders("rule-providers", v.ruleSets)
	for i, item := range nodes {
		name, _ := stringValue(mappingValue(item, "name"))
		v.validateNode(itemPath("proxies", i), "policy:"+name, item)
	}
	for i, item := range groups {
		v.validateGroup(itemPath("proxy-groups", i), item)
	}
	v.validateProviders()
	v.validateDNS()
	v.validateRules()
	v.reportCycles(v.links, "节点、代理组或订阅源形成循环依赖")
	return v.issues
}

var builtins = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL"}

func (v *validator) push(severity, path, message string) {
	v.issues = append(v.issues, Issue{Severity: severity, Path: path, Message: message})
}

func (v *validator) error(path, message string) { v.push("error", path, message) }
func (v *validator) warn(path, message string)  { v.push("warning", path, message) }

func itemPath(path string, index int) string { return path + "[" + strconv.Itoa(index) + "]" }
func childPath(path, key string) string      { return path + "[" + strconv.Quote(key) + "]" }

func (v *validator) checkSections() {
	for _, key := range []string{"dns", "proxy-providers", "rule-providers", "sub-rules", "hosts", "tun", "sniffer", "profile"} {
		if node := mappingValue(v.root, key); node != nil && node.Kind != yaml.MappingNode {
			v.error(key, "此段需要键值映射")
		}
	}
	for _, key := range []string{"proxies", "proxy-groups", "rules"} {
		if node := mappingValue(v.root, key); node != nil && node.Kind != yaml.SequenceNode {
			v.error(key, "此段需要列表")
		}
	}
	for _, key := range []string{"port", "socks-port", "redir-port", "tproxy-port", "mixed-port"} {
		v.integer(key, mappingValue(v.root, key), 0, 65535, false)
	}
	for _, key := range []string{"allow-lan", "ipv6"} {
		v.boolean(key, mappingValue(v.root, key))
	}
	v.oneOf("mode", lowerValue(mappingValue(v.root, "mode")), false, "rule", "global", "direct")
	v.oneOf("log-level", lowerValue(mappingValue(v.root, "log-level")), false, "silent", "error", "warning", "info", "debug")
	if controller := mappingValue(v.root, "external-controller"); controller != nil {
		if _, ok := stringValue(controller); !ok {
			v.error("external-controller", "控制器地址需要字符串")
		}
	}
}

func lowerValue(node *yaml.Node) *yaml.Node {
	if value, ok := stringValue(node); ok {
		lower := *node
		lower.Value = strings.ToLower(value)
		return &lower
	}
	return node
}

func (v *validator) requiredName(at string, node *yaml.Node) string {
	name := v.requiredString(at, node)
	if strings.ContainsAny(name, ",\r\n") || strings.TrimSpace(name) != name {
		v.error(at, "名称不能含逗号、换行或首尾空白")
	}
	return name
}

func (v *validator) requiredString(at string, node *yaml.Node) string {
	value, ok := stringValue(node)
	if !ok || strings.TrimSpace(value) == "" {
		v.error(at, "需要填写非空字符串")
		return ""
	}
	return value
}

func (v *validator) integer(at string, node *yaml.Node, min, max int, required bool) {
	if node == nil && !required {
		return
	}
	var value int
	if node == nil || node.ShortTag() != "!!int" || node.Decode(&value) != nil || value < min || (max >= 0 && value > max) {
		if max < 0 {
			v.error(at, fmt.Sprintf("需要大于等于 %d 的整数", min))
		} else {
			v.error(at, fmt.Sprintf("需要 %d 至 %d 的整数", min, max))
		}
	}
}

func (v *validator) boolean(at string, node *yaml.Node) bool {
	if node == nil {
		return false
	}
	var value bool
	if node.ShortTag() != "!!bool" || node.Decode(&value) != nil {
		v.error(at, "此项需要布尔值")
		return false
	}
	return value
}

func (v *validator) oneOf(at string, node *yaml.Node, required bool, options ...string) string {
	if node == nil && !required {
		return ""
	}
	value, ok := stringValue(node)
	if !ok || !slices.Contains(options, value) {
		v.error(at, "允许的值: "+strings.Join(options, "、"))
	}
	return value
}

func (v *validator) stringList(at string, node *yaml.Node, check func(string, string)) int {
	if node == nil {
		return 0
	}
	if node.Kind != yaml.SequenceNode {
		v.error(at, "此项需要列表")
		return 0
	}
	for index, item := range node.Content {
		itemAt := itemPath(at, index)
		if value := v.requiredString(itemAt, item); value != "" && check != nil {
			check(itemAt, value)
		}
	}
	return len(node.Content)
}

func (v *validator) policyRef(at, name string) {
	if !v.policies[name] {
		v.error(at, "目标策略不存在: "+name)
	}
}

func (v *validator) addPolicyEdge(key, at string, value *yaml.Node) {
	if value == nil {
		return
	}
	if name := v.requiredString(at, value); name != "" {
		v.policyRef(at, name)
		v.addEdge(key, "policy:"+name)
	}
}

func (v *validator) addEdge(from, to string) {
	if origin := v.links[from]; origin != nil {
		origin.edges = append(origin.edges, to)
	}
}

func (v *validator) reportCycles(edges graph, message string) {
	// Iterative DFS handles long valid chains without consuming the call stack.
	type frame struct {
		name string
		next int
	}
	visit := make(map[string]uint8)
	reported := make(map[string]bool)
	for _, origin := range slices.Sorted(maps.Keys(edges)) {
		if visit[origin] != 0 {
			continue
		}
		stack := []frame{{name: origin}}
		visit[origin] = 1
		for len(stack) != 0 {
			current := &stack[len(stack)-1]
			item := edges[current.name]
			if current.next == len(item.edges) {
				visit[current.name] = 2
				stack = stack[:len(stack)-1]
				continue
			}
			target := item.edges[current.next]
			current.next++
			if edges[target] == nil {
				continue
			}
			switch visit[target] {
			case 0:
				visit[target] = 1
				stack = append(stack, frame{name: target})
			case 1:
				if !reported[target] {
					v.error(edges[target].path, message)
					reported[target] = true
				}
			}
		}
	}
}

func (v *validator) validateNode(at, graphKey string, node *yaml.Node) {
	if node.Kind != yaml.MappingNode {
		return
	}
	kind := v.requiredString(at+".type", mappingValue(node, "type"))
	switch kind {
	case "socks5", "http", "ss", "ssr", "vmess", "vless", "trojan", "hysteria2", "hy2", "hysteria", "wireguard", "tuic", "ssh", "snell", "anytls", "mieru":
		v.requiredString(at+".server", mappingValue(node, "server"))
		v.integer(at+".port", mappingValue(node, "port"), 1, 65535, true)
	case "", "direct", "dns", "reject", "reject-drop", "pass":
	default:
		v.warn(at+".type", "此出站协议保留为高级配置，请用目标客户端验证")
	}
	var required []string
	switch kind {
	case "ss", "ssr":
		required = []string{"cipher", "password"}
	case "vmess", "vless":
		required = []string{"uuid"}
	case "trojan", "hysteria2", "hy2", "anytls":
		required = []string{"password"}
	}
	for _, key := range required {
		v.requiredString(at+"."+key, mappingValue(node, key))
	}
	v.addPolicyEdge(graphKey, at+".dialer-proxy", mappingValue(node, "dialer-proxy"))
}

func (v *validator) validateGroup(at string, node *yaml.Node) {
	if node.Kind != yaml.MappingNode {
		return
	}
	name, _ := stringValue(mappingValue(node, "name"))
	kind := v.requiredString(at+".type", mappingValue(node, "type"))
	switch kind {
	case "", "select", "url-test", "fallback", "load-balance":
	default:
		v.warn(at+".type", "此代理组类型需用目标客户端验证")
	}
	members := v.stringList(at+".proxies", mappingValue(node, "proxies"), func(itemAt, target string) {
		v.policyRef(itemAt, target)
		v.addEdge("policy:"+name, "policy:"+target)
	})
	providers := v.stringList(at+".use", mappingValue(node, "use"), func(itemAt, target string) {
		if !v.providers[target] {
			v.error(itemAt, "订阅源不存在: "+target)
		}
		v.addEdge("policy:"+name, "provider:"+target)
	})
	includeAll := v.boolean(at+".include-all", mappingValue(node, "include-all"))
	includeProxies := v.boolean(at+".include-all-proxies", mappingValue(node, "include-all-proxies"))
	includeProviders := v.boolean(at+".include-all-providers", mappingValue(node, "include-all-providers"))
	if members+providers == 0 && !includeAll && !includeProxies && !includeProviders &&
		slices.Contains([]string{"select", "url-test", "fallback", "load-balance", "relay", "smart"}, kind) {
		v.error(at, "请至少添加一个成员或订阅源")
	}
	if includeAll || includeProviders {
		v.includesProviders[name] = true
	}
	v.filters(at, node)
	v.httpURL(at+".url", mappingValue(node, "url"), false)
	v.integer(at+".interval", mappingValue(node, "interval"), 0, -1, false)
	v.integer(at+".tolerance", mappingValue(node, "tolerance"), 0, -1, false)
}

func (v *validator) collectProviders(section string, names map[string]bool) {
	mapping := mappingValue(v.root, section)
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		at := childPath(section, key.Value)
		name := v.requiredName(at, key)
		if name == "" {
			continue
		}
		names[name] = true
		if section == "proxy-providers" {
			v.links["provider:"+name] = &vertex{path: at}
		}
	}
}

func (v *validator) validateProviders() {
	cachePaths := make(map[string]string)
	for _, section := range []string{"proxy-providers", "rule-providers"} {
		mapping := mappingValue(v.root, section)
		if mapping == nil || mapping.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i < len(mapping.Content); i += 2 {
			name, value := mapping.Content[i].Value, mapping.Content[i+1]
			at := childPath(section, name)
			if value.Kind != yaml.MappingNode {
				v.error(at, "Provider 需要键值映射")
				continue
			}
			kind := v.oneOf(at+".type", mappingValue(value, "type"), true, "http", "file", "inline")
			v.httpURL(at+".url", mappingValue(value, "url"), kind == "http")
			v.integer(at+".interval", mappingValue(value, "interval"), 0, -1, false)
			if cache := mappingValue(value, "path"); cache != nil || kind == "file" {
				if relative := v.requiredString(at+".path", cache); relative != "" {
					normalized := path.Clean(strings.ReplaceAll(relative, "\\", "/"))
					switch {
					case normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") || path.IsAbs(normalized) || (len(normalized) > 1 && normalized[1] == ':'):
						v.warn(at+".path", "Provider 路径需对应目标客户端可读的文件")
					case cachePaths[normalized] != "":
						v.warn(at+".path", "缓存路径与另一条 Provider 重复: "+cachePaths[normalized])
					default:
						cachePaths[normalized] = at
					}
				}
			}
			if section == "proxy-providers" {
				v.validateProxyProvider(at, name, kind, value)
			} else {
				v.validateRuleProvider(at, kind, value)
			}
		}
	}
}

func (v *validator) validateProxyProvider(at, name, kind string, value *yaml.Node) {
	key := "provider:" + name
	v.filters(at, value)
	proxy := mappingValue(value, "proxy")
	v.addPolicyEdge(key, at+".proxy", proxy)
	if target, ok := stringValue(proxy); ok && v.includesProviders[target] {
		v.warn(at+".proxy", "下载策略动态包含订阅源，请确认它可在该订阅源更新前使用")
	}
	if settings := mappingValue(value, "override"); settings != nil {
		if settings.Kind != yaml.MappingNode {
			v.error(at+".override", "覆盖设置需要键值映射")
		}
		v.addPolicyEdge(key, at+".override.dialer-proxy", mappingValue(settings, "dialer-proxy"))
	}
	if kind == "inline" {
		list := mappingValue(value, "payload")
		if list == nil || list.Kind != yaml.SequenceNode || len(list.Content) == 0 {
			v.error(at+".payload", "内联订阅源需要非空的节点列表")
		}
		for index, node := range sequence(list) {
			itemAt := itemPath(at+".payload", index)
			if node.Kind != yaml.MappingNode {
				v.error(itemAt, "节点需要键值映射")
				continue
			}
			v.requiredName(itemAt+".name", mappingValue(node, "name"))
			v.validateNode(itemAt, key, node)
		}
	}
	if check := mappingValue(value, "health-check"); check != nil {
		if check.Kind != yaml.MappingNode {
			v.error(at+".health-check", "健康检查需要键值映射")
		} else {
			v.boolean(at+".health-check.enable", mappingValue(check, "enable"))
			v.httpURL(at+".health-check.url", mappingValue(check, "url"), false)
			v.integer(at+".health-check.interval", mappingValue(check, "interval"), 0, -1, false)
		}
	}
}

func (v *validator) validateRuleProvider(at, kind string, value *yaml.Node) {
	behavior := v.oneOf(at+".behavior", mappingValue(value, "behavior"), true, "domain", "ipcidr", "classical")
	format := v.oneOf(at+".format", mappingValue(value, "format"), false, "yaml", "text", "mrs")
	if format == "mrs" && behavior == "classical" {
		v.error(at+".format", "MRS 只支持 domain 或 ipcidr")
	}
	v.addPolicyEdge("", at+".proxy", mappingValue(value, "proxy"))
	if kind == "inline" && v.stringList(at+".payload", mappingValue(value, "payload"), nil) == 0 {
		v.error(at+".payload", "内联规则集需要非空的规则列表")
	}
}

func (v *validator) httpURL(at string, node *yaml.Node, required bool) {
	if node == nil && !required {
		return
	}
	value, ok := stringValue(node)
	parsed, err := url.Parse(value)
	if !ok || err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		v.error(at, "请输入有效的 HTTP 或 HTTPS URL")
	}
}

func (v *validator) filters(at string, mapping *yaml.Node) {
	for _, name := range []string{"filter", "exclude-filter"} {
		path := at + "." + name
		if node := mappingValue(mapping, name); node != nil {
			if expression, ok := stringValue(node); ok {
				v.regex(path, expression)
			} else {
				v.error(path, "筛选表达式需要字符串")
			}
		}
	}
}

func (v *validator) regex(at, expression string) {
	const maxStaticRegexBytes = 4096
	if len(expression) > maxStaticRegexBytes {
		v.warn(at, "筛选表达式较长，请用目标客户端验证")
		return
	}
	err, cached := v.regexSyntax[expression]
	if !cached {
		_, err = regexp2.Compile(expression, regexp2.None)
		v.regexSyntax[expression] = err
	}
	if err != nil {
		v.error(at, "正则表达式语法无效")
	}
}

func (v *validator) validateDNS() {
	dns := mappingValue(v.root, "dns")
	if dns == nil || dns.Kind != yaml.MappingNode {
		return
	}
	for _, key := range []string{"enable", "ipv6", "use-hosts", "use-system-hosts", "respect-rules"} {
		v.boolean("dns."+key, mappingValue(dns, key))
	}
	v.oneOf("dns.enhanced-mode", mappingValue(dns, "enhanced-mode"), false, "fake-ip", "redir-host", "normal")
	if value := mappingValue(dns, "fake-ip-range"); value != nil {
		text, _ := stringValue(value)
		if _, err := netip.ParsePrefix(text); err != nil {
			v.error("dns.fake-ip-range", "需要有效的 CIDR 网段")
		}
	}
	for _, key := range []string{"default-nameserver", "nameserver", "fallback", "proxy-server-nameserver", "direct-nameserver"} {
		v.stringList("dns."+key, mappingValue(dns, key), nil)
	}
	fakeIPMode := v.oneOf("dns.fake-ip-filter-mode", mappingValue(dns, "fake-ip-filter-mode"), false, "blacklist", "whitelist", "rule")
	v.stringList("dns.fake-ip-filter", mappingValue(dns, "fake-ip-filter"), func(at, text string) {
		v.ruleSetSelectors(at, text)
		if fakeIPMode != "rule" {
			return
		}
		if parts, err := splitRule(text); err == nil && len(parts) >= 2 && strings.EqualFold(parts[0].text, "RULE-SET") {
			if !v.ruleSets[parts[1].text] {
				v.error(at, "规则集不存在: "+parts[1].text)
			}
		}
	})
	for _, key := range []string{"nameserver-policy", "proxy-server-nameserver-policy"} {
		policy := mappingValue(dns, key)
		if policy == nil {
			continue
		}
		if policy.Kind != yaml.MappingNode {
			v.error("dns."+key, "解析策略需要键值映射")
			continue
		}
		for i := 0; i < len(policy.Content); i += 2 {
			pattern, servers := policy.Content[i].Value, policy.Content[i+1]
			at := childPath("dns."+key, pattern)
			if servers.Kind == yaml.SequenceNode {
				v.stringList(at, servers, nil)
			} else {
				v.requiredString(at, servers)
			}
			v.ruleSetSelectors(at, pattern)
		}
	}
}

func (v *validator) ruleSetSelectors(at, pattern string) {
	if names, ok := strings.CutPrefix(pattern, "rule-set:"); ok {
		for _, name := range strings.Split(names, ",") {
			if name = strings.TrimSpace(name); !v.ruleSets[name] {
				v.error(at, "规则集不存在: "+name)
			}
		}
	}
}
