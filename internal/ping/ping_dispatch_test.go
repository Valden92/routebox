package ping

import (
	"context"
	"testing"

	"github.com/Valden92/routebox/internal/subscription"
)

func TestProbeOneDispatchesByMode(t *testing.T) {
	var tcpCalls, icmpCalls int
	tcpDialFn = func(ctx context.Context, iface string, n subscription.Node) Result {
		tcpCalls++
		return Result{NodeID: n.ID, OK: true, LatencyMs: 1}
	}
	icmpPingFn = func(ctx context.Context, iface string, n subscription.Node) Result {
		icmpCalls++
		return Result{NodeID: n.ID, OK: true, LatencyMs: 2}
	}
	t.Cleanup(func() {
		tcpDialFn = tcpOne
		icmpPingFn = icmpOne
	})

	ctx := context.Background()
	_ = probeOne(ctx, "", subscription.Node{ID: "1", Protocol: "vless", Host: "h", Port: 443}, false)
	_ = probeOne(ctx, "", subscription.Node{ID: "2", Protocol: "openvpn", Network: "udp", Host: "h", Port: 1194}, false)
	_ = probeOne(ctx, "", subscription.Node{ID: "3", Protocol: "openvpn", Network: "tcp", Host: "h", Port: 443}, false)
	_ = probeOne(ctx, "", subscription.Node{ID: "4", Protocol: "vless", Host: "h", Port: 443}, true)
	_ = probeOne(ctx, "", subscription.Node{ID: "5", Protocol: "hy2", Host: "h", Port: 443}, false)

	if tcpCalls != 2 || icmpCalls != 3 {
		t.Fatalf("tcp=%d icmp=%d", tcpCalls, icmpCalls)
	}
}

func TestParsePingTime(t *testing.T) {
	out := []byte("64 bytes from 127.0.0.1: icmp_seq=1 ttl=64 time=0.412 ms\n")
	m := pingTimeRe.FindSubmatch(out)
	if len(m) != 2 || string(m[1]) != "0.412" {
		t.Fatalf("%q", m)
	}
}
