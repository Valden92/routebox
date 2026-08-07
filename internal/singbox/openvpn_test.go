package singbox

import (
	"testing"

	"github.com/Valden92/routebox/internal/subscription"
)

func TestOpenvpnEndpointRequiresAuth(t *testing.T) {
	node := subscription.Node{
		Protocol:     "openvpn",
		Host:         "vpn.example.com",
		Port:         1194,
		Network:      "udp",
		AuthUserPass: true,
		CA:           []string{"-----BEGIN CERTIFICATE-----", "X", "-----END CERTIFICATE-----"},
	}
	_, err := openvpnEndpoint("proxy", node)
	if err == nil {
		t.Fatal("expected auth error")
	}
	node.Username = "u"
	node.Password = "p"
	ep, err := openvpnEndpoint("proxy", node)
	if err != nil {
		t.Fatal(err)
	}
	if ep["type"] != "openvpn-client" || ep["tag"] != "proxy" || ep["system"] != false {
		t.Fatalf("%v", ep)
	}
	if ep["username"] != "u" || ep["password"] != "p" {
		t.Fatalf("creds %v", ep)
	}
	tls, _ := ep["tls"].(map[string]any)
	if tls == nil {
		t.Fatal("tls missing")
	}
}

func TestOpenvpnEndpointTLSCrypt(t *testing.T) {
	node := subscription.Node{
		Protocol: "openvpn",
		Host:     "vpn.example.com",
		Port:     1194,
		CA:       []string{"ca"},
		TLSCrypt: []string{"-----BEGIN OpenVPN Static key V1-----", "aa", "-----END OpenVPN Static key V1-----"},
	}
	ep, err := openvpnEndpoint("proxy", node)
	if err != nil {
		t.Fatal(err)
	}
	tls := ep["tls"].(map[string]any)
	wrap := tls["control_wrap"].(map[string]any)
	if wrap["type"] != "tls_crypt" {
		t.Fatalf("%v", wrap)
	}
}

func TestOpenvpnEndpointRejectsBadProto(t *testing.T) {
	_, err := openvpnEndpoint("proxy", subscription.Node{
		Protocol: "openvpn", Host: "h", Port: 1, Network: "sctp", CA: []string{"c"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
