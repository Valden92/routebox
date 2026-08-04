package singbox

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Valden92/routebox/internal/subscription"
)

func uriToOutbound(node subscription.Node) (map[string]any, error) {
	u, err := url.Parse(node.RawURI)
	if err != nil {
		return nil, err
	}
	tag := "proxy"
	switch u.Scheme {
	case "vless":
		return vlessOutbound(tag, u)
	case "hysteria2":
		return hysteria2Outbound(tag, u)
	case "ss":
		return shadowsocksOutbound(tag, u)
	default:
		return nil, fmt.Errorf("unsupported protocol %s", u.Scheme)
	}
}

func vlessOutbound(tag string, u *url.URL) (map[string]any, error) {
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
	if sni := q.Get("sni"); sni != "" {
		tls := map[string]any{
			"enabled":     true,
			"server_name": sni,
		}
		if q.Get("security") == "reality" {
			tls["reality"] = map[string]any{
				"enabled":    true,
				"public_key": q.Get("pbk"),
				"short_id":   q.Get("sid"),
			}
		}
		if fp := q.Get("fp"); fp != "" {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		o["tls"] = tls
	}
	if enc := q.Get("encryption"); enc != "" && enc != "none" {
		o["encryption"] = enc
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
	if sni := q.Get("sni"); sni != "" {
		o["tls"] = map[string]any{"enabled": true, "server_name": sni}
	}
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

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// PrettyPrint for debug
func PrettyPrint(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
