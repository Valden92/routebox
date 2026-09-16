package singbox

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Valden92/routebox/internal/subscription"
)

// defaultTLSFingerprint — uTLS-отпечаток, если в URI не задан fp.
// Для Reality uTLS обязателен (sing-box: «uTLS is required by reality client»).
const defaultTLSFingerprint = "chrome"

// BuildOutbound строит outbound sing-box из узла подписки.
// Для OpenVPN возвращается endpoint (isEndpoint=true) — он идёт в секцию endpoints.
// fragment включает фрагментацию TLS ClientHello (анти-DPI, аналог tlshello в Happ).
func BuildOutbound(node subscription.Node, tag string, fragment bool) (map[string]any, bool, error) {
	if strings.EqualFold(node.Protocol, "openvpn") {
		ep, err := openvpnEndpoint(tag, node)
		return ep, true, err
	}
	u, err := url.Parse(node.RawURI)
	if err != nil {
		return nil, false, err
	}
	var o map[string]any
	switch u.Scheme {
	case "vless":
		o, err = vlessOutbound(tag, u, fragment)
	case "vmess":
		o, err = vmessOutbound(tag, node.RawURI, u, fragment)
	case "trojan":
		o, err = trojanOutbound(tag, u, fragment)
	case "hysteria2", "hy2":
		o, err = hysteria2Outbound(tag, u)
	case "ss", "shadowsocks":
		o, err = shadowsocksOutbound(tag, u)
	default:
		return nil, false, fmt.Errorf("протокол %s не поддерживается", u.Scheme)
	}
	if err != nil {
		return nil, false, err
	}
	// sing-box 1.12+: резолв домена сервера через dns-direct (не через proxy).
	o["domain_resolver"] = "dns-direct"
	return o, false, nil
}

func uriToOutbound(node subscription.Node, fragment bool) (map[string]any, error) {
	o, _, err := BuildOutbound(node, "proxy", fragment)
	return o, err
}

// URIToOutbound строит outbound sing-box из RawURI узла (для тестов и отладки),
// без фрагментации.
func URIToOutbound(node subscription.Node) (map[string]any, error) {
	return uriToOutbound(node, false)
}

type tlsParams struct {
	security string // "", none, tls, reality, xtls
	sni      string
	host     string // fallback для SNI (ws/http host)
	server   string
	fp       string
	alpn     string
	insecure bool
	pbk      string
	sid      string
	fragment bool
}

// buildTLS собирает блок tls. nil — TLS не нужен (security none).
func buildTLS(p tlsParams) map[string]any {
	sec := strings.ToLower(strings.TrimSpace(p.security))
	if sec == "" || sec == "none" {
		return nil
	}
	reality := sec == "reality" || strings.TrimSpace(p.pbk) != ""
	sni := firstNonEmpty(p.sni, p.host, p.server)
	tls := map[string]any{
		"enabled":     true,
		"server_name": sni,
	}
	if reality {
		tls["reality"] = map[string]any{
			"enabled":    true,
			"public_key": p.pbk,
			"short_id":   p.sid,
		}
	}
	// uTLS нужен Reality по умолчанию и фрагментации (fragment применяется в uTLS-handshake).
	if p.fp != "" || reality || p.fragment {
		tls["utls"] = map[string]any{
			"enabled":     true,
			"fingerprint": firstNonEmpty(p.fp, defaultTLSFingerprint),
		}
	}
	if p.fragment {
		tls["fragment"] = true
	}
	if alpn := strings.TrimSpace(p.alpn); alpn != "" {
		tls["alpn"] = splitCSV(alpn)
	}
	if p.insecure {
		tls["insecure"] = true
	}
	return tls
}

// buildTransport собирает блок transport из query-параметров share-URI
// (type=ws|grpc|http|h2|httpupgrade|quic). nil — обычный TCP.
func buildTransport(q url.Values) (map[string]any, error) {
	net := strings.ToLower(strings.TrimSpace(q.Get("type")))
	switch net {
	case "", "tcp", "raw":
		return nil, nil
	case "ws":
		t := map[string]any{"type": "ws"}
		if path := q.Get("path"); path != "" {
			t["path"] = path
		}
		if host := q.Get("host"); host != "" {
			t["headers"] = map[string]any{"Host": host}
		}
		if ed := atoi(q.Get("ed")); ed > 0 {
			t["max_early_data"] = ed
			t["early_data_header_name"] = "Sec-WebSocket-Protocol"
		}
		return t, nil
	case "grpc":
		t := map[string]any{"type": "grpc"}
		if svc := q.Get("serviceName"); svc != "" {
			t["service_name"] = svc
		}
		return t, nil
	case "http", "h2":
		t := map[string]any{"type": "http"}
		if host := q.Get("host"); host != "" {
			t["host"] = splitCSV(host)
		}
		if path := q.Get("path"); path != "" {
			t["path"] = path
		}
		return t, nil
	case "httpupgrade":
		t := map[string]any{"type": "httpupgrade"}
		if host := q.Get("host"); host != "" {
			t["host"] = host
		}
		if path := q.Get("path"); path != "" {
			t["path"] = path
		}
		return t, nil
	case "quic":
		return map[string]any{"type": "quic"}, nil
	default:
		// xhttp/splithttp/kcp и прочие sing-box не умеет.
		return nil, fmt.Errorf("transport %q не поддерживается sing-box", net)
	}
}

func vlessOutbound(tag string, u *url.URL, fragment bool) (map[string]any, error) {
	q := u.Query()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	o := map[string]any{
		"type":            "vless",
		"tag":             tag,
		"server":          u.Hostname(),
		"server_port":     atoi(port),
		"uuid":            u.User.Username(),
		"packet_encoding": "xudp",
	}
	if flow := q.Get("flow"); flow != "" {
		o["flow"] = flow
	}
	security := q.Get("security")
	pbk := q.Get("pbk")
	if security == "" && (pbk != "" || q.Get("sni") != "") {
		if pbk != "" {
			security = "reality"
		} else {
			security = "tls"
		}
	}
	tls := buildTLS(tlsParams{
		security: security,
		sni:      q.Get("sni"),
		host:     q.Get("host"),
		server:   u.Hostname(),
		fp:       q.Get("fp"),
		alpn:     q.Get("alpn"),
		insecure: queryInsecure(q),
		pbk:      pbk,
		sid:      q.Get("sid"),
		fragment: fragment,
	})
	if tls != nil {
		o["tls"] = tls
	}
	if enc := q.Get("encryption"); enc != "" && enc != "none" {
		o["encryption"] = enc
	}
	t, err := buildTransport(q)
	if err != nil {
		return nil, err
	}
	if t != nil {
		o["transport"] = t
	}
	return o, nil
}

// vmessOutbound понимает обе формы share-ссылок:
// vmess://base64(JSON) (v2rayN/3x-ui) и vmess://uuid@host:port?… (URI-форма).
func vmessOutbound(tag, raw string, u *url.URL, fragment bool) (map[string]any, error) {
	if j, ok := vmessBase64Outbound(tag, raw, fragment); ok {
		return j, nil
	}
	q := u.Query()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	uid := u.User.Username()
	if uid == "" {
		return nil, fmt.Errorf("vmess: нет uuid")
	}
	o := map[string]any{
		"type":            "vmess",
		"tag":             tag,
		"server":          u.Hostname(),
		"server_port":     atoi(port),
		"uuid":            uid,
		"security":        firstNonEmpty(q.Get("encryption"), "auto"),
		"alter_id":        atoi(firstNonEmpty(q.Get("aid"), "0")),
		"packet_encoding": "xudp",
	}
	security := q.Get("security")
	pbk := q.Get("pbk")
	if security == "" && (pbk != "" || q.Get("sni") != "" || strings.EqualFold(q.Get("tls"), "true")) {
		if pbk != "" {
			security = "reality"
		} else {
			security = "tls"
		}
	}
	tls := buildTLS(tlsParams{
		security: security,
		sni:      q.Get("sni"),
		host:     q.Get("host"),
		server:   u.Hostname(),
		fp:       q.Get("fp"),
		alpn:     q.Get("alpn"),
		insecure: queryInsecure(q),
		pbk:      pbk,
		sid:      q.Get("sid"),
		fragment: fragment,
	})
	if tls != nil {
		o["tls"] = tls
	}
	t, err := buildTransport(q)
	if err != nil {
		return nil, err
	}
	if t != nil {
		o["transport"] = t
	}
	return o, nil
}

type vmessJSON struct {
	PS   string               `json:"ps"`
	Add  string               `json:"add"`
	Port subscription.FlexInt `json:"port"`
	ID   string               `json:"id"`
	Aid  subscription.FlexInt `json:"aid"`
	Scy  string               `json:"scy"`
	Net  string               `json:"net"`
	Type string               `json:"type"`
	Host string               `json:"host"`
	Path string               `json:"path"`
	TLS  string               `json:"tls"`
	SNI  string               `json:"sni"`
	ALPN string               `json:"alpn"`
	FP   string               `json:"fp"`
}

func vmessBase64Outbound(tag, raw string, fragment bool) (map[string]any, bool) {
	payload := strings.TrimPrefix(raw, "vmess://")
	if i := strings.IndexAny(payload, "#?"); i >= 0 {
		payload = payload[:i]
	}
	payload = strings.TrimSpace(payload)
	if payload == "" || strings.Contains(payload, "@") {
		return nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(payload)
	}
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(payload)
	}
	if err != nil {
		return nil, false
	}
	var j vmessJSON
	if json.Unmarshal(decoded, &j) != nil || strings.TrimSpace(j.Add) == "" || j.Port <= 0 || j.ID == "" {
		return nil, false
	}
	if strings.EqualFold(j.Net, "tcp") && strings.EqualFold(j.Type, "http") {
		// obfs-http поверх TCP sing-box не поддерживает.
		return nil, false
	}
	security := firstNonEmpty(j.Scy, "auto")
	o := map[string]any{
		"type":            "vmess",
		"tag":             tag,
		"server":          j.Add,
		"server_port":     int(j.Port),
		"uuid":            j.ID,
		"security":        security,
		"alter_id":        int(j.Aid),
		"packet_encoding": "xudp",
	}
	q := url.Values{}
	q.Set("type", j.Net)
	q.Set("path", j.Path)
	q.Set("host", j.Host)
	t, err := buildTransport(q)
	if err != nil {
		return nil, false
	}
	tlsSec := ""
	if strings.EqualFold(j.TLS, "tls") || strings.TrimSpace(j.SNI) != "" {
		tlsSec = "tls"
	}
	tls := buildTLS(tlsParams{
		security: tlsSec,
		sni:      j.SNI,
		host:     j.Host,
		server:   j.Add,
		fp:       j.FP,
		alpn:     j.ALPN,
		fragment: fragment,
	})
	if tls != nil {
		o["tls"] = tls
	}
	if t != nil {
		o["transport"] = t
	}
	return o, true
}

func trojanOutbound(tag string, u *url.URL, fragment bool) (map[string]any, error) {
	q := u.Query()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	pass := u.User.Username()
	if pass == "" {
		return nil, fmt.Errorf("trojan: нет пароля")
	}
	o := map[string]any{
		"type":        "trojan",
		"tag":         tag,
		"server":      u.Hostname(),
		"server_port": atoi(port),
		"password":    pass,
	}
	security := q.Get("security")
	if security == "" {
		// trojan без явного security работает поверх TLS.
		security = "tls"
	}
	pbk := q.Get("pbk")
	tls := buildTLS(tlsParams{
		security: security,
		sni:      q.Get("sni"),
		host:     q.Get("host"),
		server:   u.Hostname(),
		fp:       q.Get("fp"),
		alpn:     q.Get("alpn"),
		insecure: queryInsecure(q),
		pbk:      pbk,
		sid:      q.Get("sid"),
		fragment: fragment,
	})
	if tls != nil {
		o["tls"] = tls
	}
	t, err := buildTransport(q)
	if err != nil {
		return nil, err
	}
	if t != nil {
		o["transport"] = t
	}
	return o, nil
}

func hysteria2Outbound(tag string, u *url.URL) (map[string]any, error) {
	q := u.Query()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	o := map[string]any{
		"type":        "hysteria2",
		"tag":         tag,
		"server":      u.Hostname(),
		"server_port": atoi(port),
		"password":    u.User.Username(),
	}
	if obfs := q.Get("obfs"); obfs != "" {
		o["obfs"] = map[string]any{
			"type":     obfs,
			"password": q.Get("obfs-password"),
		}
	}
	tlsParams := map[string]any{"enabled": true}
	if sni := q.Get("sni"); sni != "" {
		tlsParams["server_name"] = sni
	}
	if queryInsecure(q) {
		tlsParams["insecure"] = true
	}
	if alpn := strings.TrimSpace(q.Get("alpn")); alpn != "" {
		tlsParams["alpn"] = splitCSV(alpn)
	}
	o["tls"] = tlsParams
	return o, nil
}

func shadowsocksOutbound(tag string, u *url.URL) (map[string]any, error) {
	// ss://method:pass@host:port
	user := u.User.String()
	method := user
	pass := ""
	if u.User != nil {
		method = u.User.Username()
		pass, _ = u.User.Password()
	}
	if i := strings.Index(user, ":"); i >= 0 && pass == "" {
		method = user[:i]
		pass = user[i+1:]
	}
	port := u.Port()
	if port == "" {
		port = "8388"
	}
	return map[string]any{
		"type":        "shadowsocks",
		"tag":         tag,
		"server":      u.Hostname(),
		"server_port": atoi(port),
		"method":      method,
		"password":    pass,
	}, nil
}

func queryInsecure(q url.Values) bool {
	for _, k := range []string{"allowInsecure", "allow_insecure", "insecure", "skipCertVerify"} {
		v := strings.ToLower(q.Get(k))
		if v == "1" || v == "true" {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// PrettyPrint for debug
func PrettyPrint(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
