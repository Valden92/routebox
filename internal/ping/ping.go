package ping

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Valden92/routebox/internal/network"
	"github.com/Valden92/routebox/internal/subscription"
)

type Result struct {
	NodeID    string  `json:"nodeId"`
	Host      string  `json:"host"`
	Port      int     `json:"port"`
	LatencyMs float64 `json:"latencyMs"`
	OK        bool    `json:"ok"`
	Error     string  `json:"error,omitempty"`
}

func TCPBatch(ctx context.Context, iface string, nodes []subscription.Node, concurrency int) []Result {
	if concurrency <= 0 {
		concurrency = 20
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	results := make([]Result, len(nodes))
	for i, n := range nodes {
		wg.Add(1)
		go func(i int, n subscription.Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = probeOne(ctx, iface, n)
		}(i, n)
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool {
		if results[i].OK != results[j].OK {
			return results[i].OK
		}
		return results[i].LatencyMs < results[j].LatencyMs
	})
	return results
}

// ProbeMode — как проверять доступность узла.
func ProbeMode(n subscription.Node) string {
	if strings.EqualFold(n.Protocol, "openvpn") {
		netw := strings.ToLower(strings.TrimSpace(n.Network))
		if netw == "tcp" {
			return "tcp"
		}
		// UDP OpenVPN: порт не слушает TCP; сырой UDP без tls-auth HMAC молчит.
		// Проверяем ICMP до host (как «жив ли сервер»).
		return "icmp"
	}
	return "tcp"
}

func probeOne(ctx context.Context, iface string, n subscription.Node) Result {
	switch ProbeMode(n) {
	case "icmp":
		return icmpPingFn(ctx, iface, n)
	default:
		return tcpDialFn(ctx, iface, n)
	}
}

// Хуки для тестов (подмена dial/ping без сети).
var (
	tcpDialFn  = tcpOne
	icmpPingFn = icmpOne
)

func tcpOne(ctx context.Context, iface string, n subscription.Node) Result {
	r := Result{NodeID: n.ID, Host: n.Host, Port: n.Port}
	addr := fmt.Sprintf("%s:%d", n.Host, n.Port)
	d := &net.Dialer{Timeout: 4 * time.Second}
	if iface != "" {
		d.Control = network.BindControlExport(iface)
	}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", addr)
	r.LatencyMs = float64(time.Since(start).Milliseconds())
	if err != nil {
		r.Error = err.Error()
		return r
	}
	_ = conn.Close()
	r.OK = true
	return r
}

var pingTimeRe = regexp.MustCompile(`(?i)time[=<]([\d.]+)\s*ms`)

func icmpOne(ctx context.Context, iface string, n subscription.Node) Result {
	r := Result{NodeID: n.ID, Host: n.Host, Port: n.Port}
	if n.Host == "" {
		r.Error = "empty host"
		return r
	}
	args := []string{"-c", "1", "-W", "2"}
	if iface != "" {
		args = append(args, "-I", iface)
	}
	args = append(args, n.Host)
	cmd := exec.CommandContext(ctx, "ping", args...)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := float64(time.Since(start).Milliseconds())
	if err != nil {
		r.LatencyMs = elapsed
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		r.Error = msg
		return r
	}
	r.OK = true
	if m := pingTimeRe.FindSubmatch(out); len(m) == 2 {
		if ms, err := strconv.ParseFloat(string(m[1]), 64); err == nil {
			r.LatencyMs = ms
			return r
		}
	}
	r.LatencyMs = elapsed
	return r
}
