package singbox

import (
	"fmt"
	"strings"

	"github.com/Valden92/routebox/internal/subscription"
)

// openvpnEndpoint собирает endpoints[] entry type=openvpn-client с tag=proxy.
// system=false — без второго OS-TUN; route_no_pull — маршруты только через tun100/coexist.
func openvpnEndpoint(tag string, node subscription.Node) (map[string]any, error) {
	if strings.ToLower(node.Protocol) != "openvpn" {
		return nil, fmt.Errorf("не OpenVPN-узел")
	}
	if node.Host == "" || node.Port <= 0 {
		return nil, fmt.Errorf("OpenVPN: нет remote host/port")
	}
	if len(node.CA) == 0 {
		return nil, fmt.Errorf("OpenVPN: нет CA")
	}
	network := strings.ToLower(strings.TrimSpace(node.Network))
	if network == "" {
		network = "udp"
	}
	if network != "udp" && network != "tcp" {
		return nil, fmt.Errorf("OpenVPN: неподдерживаемый proto %q", network)
	}
	if node.AuthUserPass && (strings.TrimSpace(node.Username) == "" || node.Password == "") {
		return nil, fmt.Errorf("OpenVPN: нужны логин и пароль (auth-user-pass)")
	}

	tls := map[string]any{
		"certificate": node.CA,
	}
	if len(node.Cert) > 0 {
		tls["client_certificate"] = node.Cert
	}
	if len(node.Key) > 0 {
		tls["client_key"] = node.Key
	}
	if wrap := controlWrap(node); wrap != nil {
		tls["control_wrap"] = wrap
	}

	ep := map[string]any{
		"type":            "openvpn-client",
		"tag":             tag,
		"mode":            "tls",
		"server":          node.Host,
		"server_port":     node.Port,
		"network":         network,
		"system":          false,
		"route_no_pull":   true,
		"domain_resolver": "dns-direct",
		"pull_filters": []map[string]any{
			{"action": "ignore", "text": "route"},
			{"action": "ignore", "text": "dhcp-option"},
			{"action": "ignore", "text": "redirect-gateway"},
			{"action": "ignore", "text": "block-ipv6"},
		},
		"tls": tls,
	}
	if node.AuthUserPass {
		ep["username"] = node.Username
		ep["password"] = node.Password
	}
	return ep, nil
}

func controlWrap(node subscription.Node) map[string]any {
	if len(node.TLSCrypt) > 0 {
		return map[string]any{
			"type": "tls_crypt",
			"key":  node.TLSCrypt,
		}
	}
	if len(node.TLSAuth) > 0 {
		w := map[string]any{
			"type": "tls_auth",
			"key":  node.TLSAuth,
		}
		dir := strings.TrimSpace(node.KeyDirection)
		if dir == "0" || strings.EqualFold(dir, "server") {
			w["direction"] = "server"
		} else if dir == "1" || strings.EqualFold(dir, "client") {
			w["direction"] = "client"
		}
		return w
	}
	return nil
}
