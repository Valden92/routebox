package subscription_test

import (
	"testing"

	"github.com/Valden92/routebox/internal/subscription"
)

func TestEndpointLabel(t *testing.T) {
	got := subscription.EndpointLabel(subscription.Node{
		Protocol: "openvpn", Host: "vpn.example.com", Port: 1194, Network: "udp",
	})
	if got != "OPENVPN · vpn.example.com:1194 · UDP" {
		t.Fatalf("%q", got)
	}
	got = subscription.EndpointLabel(subscription.Node{
		Protocol: "openvpn", Host: "vpn.example.com", Port: 443, Network: "tcp", AuthUserPass: true,
	})
	if got != "OPENVPN · vpn.example.com:443 · TCP · login" {
		t.Fatalf("%q", got)
	}
}
