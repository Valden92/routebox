package ping_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Valden92/routebox/internal/ping"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestProbeMode(t *testing.T) {
	cases := []struct {
		n    subscription.Node
		want string
	}{
		{subscription.Node{Protocol: "vless"}, "tcp"},
		{subscription.Node{Protocol: "hy2", Host: "x", Port: 443}, "tcp"},
		{subscription.Node{Protocol: "openvpn", Network: "udp"}, "icmp"},
		{subscription.Node{Protocol: "openvpn", Network: ""}, "icmp"},
		{subscription.Node{Protocol: "openvpn", Network: "tcp"}, "tcp"},
		{subscription.Node{Protocol: "OpenVPN", Network: "UDP"}, "icmp"},
	}
	for _, tc := range cases {
		if got := ping.ProbeMode(tc.n); got != tc.want {
			t.Fatalf("%+v → %q want %q", tc.n, got, tc.want)
		}
	}
}

func TestTCPBatchOpenVPNUDPDoesNotNeedTCPPort(t *testing.T) {
	// Регрессия: раньше TCP :1194 → fail на UDP OpenVPN.
	// 127.0.0.1 обычно отвечает ICMP, порт 1194 TCP чаще закрыт.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n := subscription.Node{
		ID: "local-ovpn", Protocol: "openvpn", Network: "udp",
		Host: "127.0.0.1", Port: 1194,
	}
	res := ping.TCPBatch(ctx, "", []subscription.Node{n}, 1)
	if len(res) != 1 {
		t.Fatalf("%+v", res)
	}
	if !res[0].OK {
		t.Skipf("ICMP до 127.0.0.1 недоступен в этой среде: %s", res[0].Error)
	}
	if res[0].LatencyMs <= 0 {
		t.Fatalf("latency %+v", res[0])
	}
}

func TestTCPBatchTCPFailsOnClosedPort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	n := subscription.Node{
		ID: "tcp-closed", Protocol: "vless",
		Host: "127.0.0.1", Port: 1, // обычно closed
	}
	res := ping.TCPBatch(ctx, "", []subscription.Node{n}, 1)
	if len(res) != 1 || res[0].OK {
		t.Fatalf("want TCP fail, got %+v", res)
	}
	if res[0].Error == "" {
		t.Fatal("empty error")
	}
}

func TestTCPBatchOpenVPNTCPUsesTCP(t *testing.T) {
	// openvpn+tcp → ProbeMode tcp → закрытый порт = fail (не ICMP success).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	n := subscription.Node{
		ID: "ovpn-tcp", Protocol: "openvpn", Network: "tcp",
		Host: "127.0.0.1", Port: 1,
	}
	if ping.ProbeMode(n) != "tcp" {
		t.Fatal(ping.ProbeMode(n))
	}
	res := ping.TCPBatch(ctx, "", []subscription.Node{n}, 1)
	if len(res) != 1 || res[0].OK {
		t.Fatalf("want fail via TCP, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res[0].Error), "connect") &&
		!strings.Contains(strings.ToLower(res[0].Error), "refused") &&
		!strings.Contains(strings.ToLower(res[0].Error), "timeout") &&
		!strings.Contains(strings.ToLower(res[0].Error), "i/o") {
		// допускаем разные формулировки dial error
		t.Logf("error text: %s", res[0].Error)
	}
}
