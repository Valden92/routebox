package ping

import (
	"context"
	"fmt"
	"net"
	"sort"
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
			results[i] = tcpOne(ctx, iface, n)
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
