package config

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Fetcher retrieves an upstream YAML using a transport that only connects to public addresses.
type Fetcher struct{}

func (*Fetcher) Fetch(ctx context.Context, rawURL, userAgent string) (string, error) {
	source, err := importURL(rawURL)
	if err != nil {
		return "", err
	}
	if len(userAgent) > 256 || strings.IndexFunc(userAgent, func(c rune) bool { return c < 32 || c == 127 }) >= 0 {
		return "", errors.New("User-Agent 过长或含无效字符")
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "ClashConfigStudio/1.0"
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return "", errors.New("订阅地址无效")
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "application/yaml, text/yaml, text/plain, application/octet-stream, */*;q=0.2")
	transport := &http.Transport{
		DialContext:            publicDial,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  6 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: checkRedirect}
	return fetchYAML(client, request)
}

func fetchYAML(client *http.Client, request *http.Request) (string, error) {
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, errNonPublic) {
			return "", errNonPublic
		}
		if errors.Is(err, errRedirect) {
			return "", errRedirect
		}
		// http.Client errors may embed the entire upstream URL, including subscription tokens.
		return "", errors.New("无法连接订阅地址或上游响应超时")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("上游返回 HTTP %d", response.StatusCode)
	}
	if kind := strings.ToLower(response.Header.Get("Content-Type")); strings.Contains(kind, "text/html") || strings.Contains(kind, "application/xhtml+xml") {
		return "", errors.New("上游返回网页，请提供 Clash/Mihomo YAML 订阅地址")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxYAMLBytes+1))
	if err != nil {
		return "", errors.New("读取上游 YAML 失败")
	}
	if len(data) > MaxYAMLBytes {
		return "", errors.New("上游 YAML 大小超过 2 MiB")
	}
	return string(data), nil
}

var errNonPublic = errors.New("上游订阅地址必须指向公网服务器")
var errRedirect = errors.New("上游订阅重定向无效或超过限制")

func importURL(raw string) (*url.URL, error) {
	if len(raw) > 8192 {
		return nil, errors.New("订阅地址过长")
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Opaque != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("请输入不含账号信息或片段的 HTTP 或 HTTPS 地址")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return nil, errNonPublic
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !publicAddress(addr) {
			return nil, errNonPublic
		}
	} else if !strings.Contains(host, ".") {
		return nil, errNonPublic
	}
	return parsed, nil
}

func checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) > 4 {
		return errRedirect
	}
	request.Header.Del("Referer")
	if _, err := importURL(request.URL.String()); err != nil {
		if errors.Is(err, errNonPublic) {
			return err
		}
		return errRedirect
	}
	return nil
}

var blockedRanges = parsePrefixes(
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
	"192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
	"2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20",
)

var globalIPv6 = netip.MustParsePrefix("2000::/3")

func parsePrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, len(values))
	for i, value := range values {
		prefixes[i] = netip.MustParsePrefix(value)
	}
	return prefixes
}

func publicAddress(addr netip.Addr) bool {
	if addr.Zone() != "" {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || (addr.Is6() && !globalIPv6.Contains(addr)) {
		return false
	}
	for _, prefix := range blockedRanges {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

type ipResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

func publicAddresses(ctx context.Context, resolver ipResolver, host string) ([]netip.Addr, error) {
	resolved := []netip.Addr{}
	if addr, err := netip.ParseAddr(host); err == nil {
		resolved = append(resolved, addr)
	} else {
		addresses, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("无法解析订阅服务器")
		}
		resolved = addresses
	}
	for _, addr := range resolved {
		if !publicAddress(addr) {
			return nil, errNonPublic
		}
	}
	return resolved, nil
}

func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("订阅连接地址无效")
	}
	addresses, err := publicAddresses(ctx, net.DefaultResolver, host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	for _, addr := range addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("无法连接订阅服务器")
}

func ParseImport(source, mode string) (*Document, error) {
	if mode != "config" && mode != "provider" {
		return nil, errors.New("请选择完整配置或节点订阅")
	}
	doc, err := Parse(source)
	if err != nil {
		return nil, errors.New("无法读取订阅，请提供格式正确的 Clash/Mihomo YAML")
	}
	if mode == "provider" {
		nodes := sequence(mappingValue(doc.root, "proxies"))
		if len(nodes) == 0 {
			return nil, errors.New("节点订阅需包含非空 proxies 列表")
		}
		for _, node := range nodes {
			name, _ := stringValue(mappingValue(node, "name"))
			protocol, _ := stringValue(mappingValue(node, "type"))
			if node.Kind != yaml.MappingNode || strings.TrimSpace(name) == "" || strings.TrimSpace(protocol) == "" {
				return nil, errors.New("节点订阅的条目需要名称与类型")
			}
		}
		return doc, nil
	}
	for _, key := range strings.Fields("mode port socks-port mixed-port redir-port tproxy-port allow-lan bind-address log-level ipv6 " +
		"external-controller proxies proxy-providers proxy-groups rules rule-providers sub-rules dns tun sniffer hosts profile listeners " +
		"geodata-mode geox-url ntp routing-mark interface-name tcp-concurrent authentication find-process-mode external-ui") {
		if mappingValue(doc.root, key) != nil {
			return doc, nil
		}
	}
	return nil, errors.New("订阅里没有识别到 Clash/Mihomo 配置字段")
}
