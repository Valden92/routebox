package subscription_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestLooksLikeClash(t *testing.T) {
	t.Parallel()
	if !subscription.LooksLikeClash("proxies:\n- type: ss\n  server: a\n") {
		t.Fatal("expected clash")
	}
	if subscription.LooksLikeClash("vless://x@y:1") {
		t.Fatal("uri is not clash")
	}
}

func TestParseClashSample(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("testdata", "clash-sample.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	nodes, skipped, err := subscription.ParseClash(raw)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Fatalf("skipped trojan want 1, got %d", skipped)
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes %d", len(nodes))
	}
	byProto := map[string]subscription.Node{}
	for _, n := range nodes {
		byProto[n.Protocol] = n
	}
	vless := byProto["vless"]
	if vless.Host != "203.0.113.10" || vless.Port != 443 {
		t.Fatalf("vless endpoint %+v", vless)
	}
	if !strings.Contains(vless.RawURI, "security=reality") || !strings.Contains(vless.RawURI, "pbk=") {
		t.Fatalf("vless uri %s", vless.RawURI)
	}
	if _, err := singbox.URIToOutbound(vless); err != nil {
		t.Fatalf("vless outbound: %v", err)
	}
	ss := byProto["ss"]
	if ss.Host != "203.0.113.20" || !strings.HasPrefix(ss.RawURI, "ss://") {
		t.Fatalf("ss %+v", ss)
	}
	if _, err := singbox.URIToOutbound(ss); err != nil {
		t.Fatalf("ss outbound: %v", err)
	}
	hy := byProto["hysteria2"]
	if hy.Host != "203.0.113.30" || !strings.Contains(hy.RawURI, "sni=hy2.example.com") {
		t.Fatalf("hy2 %+v", hy)
	}
	if _, err := singbox.URIToOutbound(hy); err != nil {
		t.Fatalf("hy2 outbound: %v", err)
	}
}

func TestParseBodyDetectsClash(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("testdata", "clash-sample.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := subscription.ParseBody(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("ParseBody clash nodes %d", len(nodes))
	}
}

func TestParseClashEmptyProxies(t *testing.T) {
	t.Parallel()
	_, _, err := subscription.ParseClash([]byte("proxies: []\n"))
	if err == nil {
		t.Fatal("want error")
	}
}
