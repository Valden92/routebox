package subscription

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// LooksLikeClash — эвристика Clash/Mihomo YAML (секция proxies).
func LooksLikeClash(text string) bool {
	low := strings.ToLower(text)
	if len(low) > 4096 {
		low = low[:4096]
	}
	if !strings.Contains(low, "proxies:") {
		return false
	}
	return strings.Contains(low, "type:") && strings.Contains(low, "server:")
}

// ParseClash разбирает Clash/Mihomo YAML: только proxies → Node с каноническим rawUri.
// Неподдерживаемые типы увеличивают skipped (proxy-groups/rules игнорируются).
func ParseClash(data []byte) (nodes []Node, skipped int, err error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, 0, fmt.Errorf("пустой Clash YAML")
	}
	var root map[string]any
	if err := yaml.Unmarshal([]byte(text), &root); err != nil {
		return nil, 0, fmt.Errorf("некорректный Clash YAML: %w", err)
	}
	raw, ok := root["proxies"]
	if !ok || raw == nil {
		return nil, 0, fmt.Errorf("в Clash YAML нет секции proxies")
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, 0, fmt.Errorf("секция proxies должна быть списком")
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			skipped++
			continue
		}
		uri, perr := clashProxyToURI(m)
		if perr != nil || uri == "" {
			skipped++
			continue
		}
		n, nerr := parseURI(uri)
		if nerr != nil || !ValidNode(n) {
			skipped++
			continue
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		if skipped > 0 {
			return nil, skipped, fmt.Errorf("в Clash YAML нет поддерживаемых прокси (vless/ss/hysteria2); пропущено %d", skipped)
		}
		return nil, 0, fmt.Errorf("в Clash YAML список proxies пуст")
	}
	return nodes, skipped, nil
}

func clashProxyToURI(m map[string]any) (string, error) {
	typ := strings.ToLower(strings.TrimSpace(clashString(m, "type")))
	server := strings.TrimSpace(clashString(m, "server"))
	port := clashInt(m, "port")
	name := strings.TrimSpace(clashString(m, "name"))
	if server == "" || port <= 0 {
		return "", fmt.Errorf("нет server/port")
	}
	switch typ {
	case "vless":
		return clashVLESSURI(m, server, port, name)
	case "vmess":
		return clashVmessURI(m, server, port, name)
	case "trojan":
		return clashTrojanURI(m, server, port, name)
	case "ss", "shadowsocks":
		return clashSSURI(m, server, port, name)
	case "hysteria2", "hy2":
		return clashHysteria2URI(m, server, port, name)
	default:
		return "", fmt.Errorf("unsupported type %s", typ)
	}
}

// setTransportQuery переносит transport-настройки Clash (ws-opts/h2-opts/grpc-opts)
// в query-параметры канонического share-URI.
func setTransportQuery(q url.Values, m map[string]any, network string) {
	switch network {
	case "ws", "http", "h2", "grpc", "httpupgrade":
		if path := clashNestedString(m, "ws-opts", "path"); path != "" {
			q.Set("path", path)
		}
		if path := clashNestedString(m, "h2-opts", "path"); path != "" {
			q.Set("path", path)
		}
		if path := clashNestedString(m, "http-opts", "path"); path != "" {
			q.Set("path", path)
		}
		if svc := clashNestedString(m, "grpc-opts", "grpc-service-name"); svc != "" {
			q.Set("serviceName", svc)
		}
		if host := clashWSHost(m); host != "" {
			q.Set("host", host)
		}
	}
}

func clashVLESSURI(m map[string]any, server string, port int, name string) (string, error) {
	uuid := strings.TrimSpace(clashString(m, "uuid"))
	if uuid == "" {
		return "", fmt.Errorf("vless: нет uuid")
	}
	q := url.Values{}
	q.Set("encryption", "none")
	if flow := clashString(m, "flow"); flow != "" {
		q.Set("flow", flow)
	}
	network := strings.ToLower(clashString(m, "network"))
	if network == "" {
		network = "tcp"
	}
	q.Set("type", network)
	setTransportQuery(q, m, network)
	tlsEnabled := clashBool(m, "tls") || strings.EqualFold(clashString(m, "security"), "reality") || clashMap(m, "reality-opts") != nil
	sni := firstNonEmpty(clashString(m, "servername"), clashString(m, "sni"))
	fp := firstNonEmpty(clashString(m, "client-fingerprint"), clashString(m, "fingerprint"))
	reality := clashMap(m, "reality-opts")
	if reality != nil {
		q.Set("security", "reality")
		if pbk := clashString(reality, "public-key"); pbk != "" {
			q.Set("pbk", pbk)
		}
		if sid := clashString(reality, "short-id"); sid != "" {
			q.Set("sid", sid)
		}
		if sni != "" {
			q.Set("sni", sni)
		}
		if fp != "" {
			q.Set("fp", fp)
		}
	} else if tlsEnabled {
		q.Set("security", "tls")
		if sni != "" {
			q.Set("sni", sni)
		}
		if fp != "" {
			q.Set("fp", fp)
		}
	} else {
		q.Set("security", "none")
	}
	u := &url.URL{
		Scheme:   "vless",
		User:     url.User(uuid),
		Host:     fmt.Sprintf("%s:%d", server, port),
		RawQuery: q.Encode(),
		Fragment: name,
	}
	return u.String(), nil
}

func clashVmessURI(m map[string]any, server string, port int, name string) (string, error) {
	uid := strings.TrimSpace(clashString(m, "uuid"))
	if uid == "" {
		return "", fmt.Errorf("vmess: нет uuid")
	}
	q := url.Values{}
	q.Set("encryption", firstNonEmpty(clashString(m, "cipher"), "auto"))
	if aid := clashInt(m, "alterId"); aid > 0 {
		q.Set("aid", strconv.Itoa(aid))
	}
	network := strings.ToLower(clashString(m, "network"))
	if network == "" {
		network = "tcp"
	}
	q.Set("type", network)
	setTransportQuery(q, m, network)
	sni := firstNonEmpty(clashString(m, "servername"), clashString(m, "sni"))
	fp := firstNonEmpty(clashString(m, "client-fingerprint"), clashString(m, "fingerprint"))
	reality := clashMap(m, "reality-opts")
	if reality != nil {
		q.Set("security", "reality")
		if pbk := clashString(reality, "public-key"); pbk != "" {
			q.Set("pbk", pbk)
		}
		if sid := clashString(reality, "short-id"); sid != "" {
			q.Set("sid", sid)
		}
		if sni != "" {
			q.Set("sni", sni)
		}
		if fp != "" {
			q.Set("fp", fp)
		}
	} else if clashBool(m, "tls") {
		q.Set("security", "tls")
		if sni != "" {
			q.Set("sni", sni)
		}
		if fp != "" {
			q.Set("fp", fp)
		}
	} else {
		q.Set("security", "none")
	}
	if clashBool(m, "skip-cert-verify") {
		q.Set("allowInsecure", "1")
	}
	u := &url.URL{
		Scheme:   "vmess",
		User:     url.User(uid),
		Host:     fmt.Sprintf("%s:%d", server, port),
		RawQuery: q.Encode(),
		Fragment: name,
	}
	return u.String(), nil
}

func clashTrojanURI(m map[string]any, server string, port int, name string) (string, error) {
	pass := clashString(m, "password")
	if pass == "" {
		return "", fmt.Errorf("trojan: нет password")
	}
	q := url.Values{}
	network := strings.ToLower(clashString(m, "network"))
	if network != "" && network != "tcp" {
		q.Set("type", network)
		setTransportQuery(q, m, network)
	}
	sni := firstNonEmpty(clashString(m, "servername"), clashString(m, "sni"))
	fp := firstNonEmpty(clashString(m, "client-fingerprint"), clashString(m, "fingerprint"))
	if reality := clashMap(m, "reality-opts"); reality != nil {
		q.Set("security", "reality")
		if pbk := clashString(reality, "public-key"); pbk != "" {
			q.Set("pbk", pbk)
		}
		if sid := clashString(reality, "short-id"); sid != "" {
			q.Set("sid", sid)
		}
		if sni != "" {
			q.Set("sni", sni)
		}
		if fp != "" {
			q.Set("fp", fp)
		}
	} else {
		// trojan по умолчанию работает поверх TLS.
		q.Set("security", "tls")
		if sni != "" {
			q.Set("sni", sni)
		}
		if fp != "" {
			q.Set("fp", fp)
		}
	}
	if clashBool(m, "skip-cert-verify") {
		q.Set("allowInsecure", "1")
	}
	u := &url.URL{
		Scheme:   "trojan",
		User:     url.User(pass),
		Host:     fmt.Sprintf("%s:%d", server, port),
		RawQuery: q.Encode(),
		Fragment: name,
	}
	return u.String(), nil
}

func clashSSURI(m map[string]any, server string, port int, name string) (string, error) {
	method := firstNonEmpty(clashString(m, "cipher"), clashString(m, "method"))
	pass := clashString(m, "password")
	if method == "" || pass == "" {
		return "", fmt.Errorf("ss: нет cipher/password")
	}
	u := &url.URL{
		Scheme:   "ss",
		User:     url.UserPassword(method, pass),
		Host:     fmt.Sprintf("%s:%d", server, port),
		Fragment: name,
	}
	return u.String(), nil
}

func clashHysteria2URI(m map[string]any, server string, port int, name string) (string, error) {
	pass := firstNonEmpty(clashString(m, "password"), clashString(m, "auth"))
	if pass == "" {
		return "", fmt.Errorf("hysteria2: нет password")
	}
	q := url.Values{}
	sni := firstNonEmpty(clashString(m, "sni"), clashString(m, "servername"))
	if sni == "" {
		if tls := clashMap(m, "tls"); tls != nil {
			sni = clashString(tls, "servername")
		}
	}
	if sni != "" {
		q.Set("sni", sni)
	}
	u := &url.URL{
		Scheme:   "hysteria2",
		User:     url.User(pass),
		Host:     fmt.Sprintf("%s:%d", server, port),
		RawQuery: q.Encode(),
		Fragment: name,
	}
	return u.String(), nil
}

func clashWSHost(m map[string]any) string {
	if h := clashNestedString(m, "ws-opts", "headers", "Host"); h != "" {
		return h
	}
	if h := clashNestedString(m, "ws-opts", "headers", "host"); h != "" {
		return h
	}
	return ""
}

func clashString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(t)
	}
}

func clashInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func clashBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	case int, int64, float64:
		return clashInt(m, key) != 0
	default:
		return false
	}
}

func clashMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	out, _ := v.(map[string]any)
	return out
}

func clashNestedString(m map[string]any, keys ...string) string {
	cur := m
	for i, k := range keys {
		if i == len(keys)-1 {
			return clashString(cur, k)
		}
		next := clashMap(cur, k)
		if next == nil {
			return ""
		}
		cur = next
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
