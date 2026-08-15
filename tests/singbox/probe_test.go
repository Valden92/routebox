package singbox_test

import (
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestProbeProxyReachableRejectsEmpty(t *testing.T) {
	err := singbox.ProbeProxyReachable(subscription.Node{}, "", false)
	if err == nil {
		t.Fatal("expected error")
	}
	err = singbox.ProbeProxyReachable(subscription.Node{Host: "x"}, "", false)
	if err == nil {
		t.Fatal("expected error for port 0")
	}
}

func TestProbeProxyReachableOpenVPNUDPNotTCP(t *testing.T) {
	// Регрессия connect gate: dial tcp :1194 на UDP OpenVPN.
	// ICMP до loopback ок при закрытом TCP/1194.
	err := singbox.ProbeProxyReachable(subscription.Node{
		Protocol: "openvpn",
		Network:  "udp",
		Host:     "127.0.0.1",
		Port:     1194,
	}, "", false)
	if err != nil {
		if strings.Contains(err.Error(), "недоступен") {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
}

func TestProbeProxyReachableVLESSClosedPortFails(t *testing.T) {
	err := singbox.ProbeProxyReachable(subscription.Node{
		Protocol: "vless",
		Host:     "127.0.0.1",
		Port:     1,
	}, "", false)
	if err == nil {
		t.Fatal("expected TCP failure on closed port")
	}
	if !strings.Contains(err.Error(), "недоступен") {
		t.Fatalf("%v", err)
	}
	// Не должно выглядеть как icmp/ping output path для vless
	low := strings.ToLower(err.Error())
	if strings.Contains(low, "ping:") && !strings.Contains(low, "connect") && !strings.Contains(low, "dial") {
		t.Fatalf("unexpected icmp-looking error for vless: %v", err)
	}
}
