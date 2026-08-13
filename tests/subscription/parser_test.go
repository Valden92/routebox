package subscription_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/Valden92/routebox/internal/subscription"
)

func TestIsRemoteURL(t *testing.T) {
	if !subscription.IsRemoteURL("https://example.com/sub") {
		t.Fatal("https")
	}
	if !subscription.IsRemoteURL(" http://example.com ") {
		t.Fatal("http")
	}
	if subscription.IsRemoteURL("") || subscription.IsRemoteURL("vless://x") {
		t.Fatal("local")
	}
}

func TestParseBodyPlainLines(t *testing.T) {
	body := []byte("" +
		"# comment\n" +
		"vless://11111111-1111-1111-1111-111111111111@node.example.com:443?encryption=none&security=tls&sni=node.example.com#Alpha\n" +
		"hysteria2://secret@hy2.example.com:8443?sni=hy2.example.com#Bravo\n" +
		"not-a-uri\n" +
		"ss://YWVzLTI1Ni1nY206cGFzcw@ss.example.com:8388#Charlie\n")

	nodes, err := subscription.ParseBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("got %d nodes: %+v", len(nodes), nodes)
	}
	// parseLines sorts by name
	if nodes[0].Name != "Alpha" || nodes[0].Protocol != "vless" || nodes[0].Port != 443 {
		t.Fatalf("Alpha: %+v", nodes[0])
	}
	if nodes[1].Name != "Bravo" || nodes[1].Protocol != "hysteria2" {
		t.Fatalf("Bravo: %+v", nodes[1])
	}
	if nodes[2].Name != "Charlie" || nodes[2].Protocol != "ss" {
		t.Fatalf("Charlie: %+v", nodes[2])
	}
}

func TestParseBodyBase64(t *testing.T) {
	plain := "vless://22222222-2222-2222-2222-222222222222@b64.example.com:443?encryption=none#B64\n"
	enc := base64.StdEncoding.EncodeToString([]byte(plain))
	nodes, err := subscription.ParseBody([]byte(enc))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Host != "b64.example.com" {
		t.Fatalf("%+v", nodes)
	}
}

func TestParseBodyHTML(t *testing.T) {
	_, err := subscription.ParseBody([]byte("<!DOCTYPE html><html><meta/><script></script>"))
	if err == nil {
		t.Fatal("expected HTML error")
	}
}

func TestParseBodyFromTestdata(t *testing.T) {
	path := filepath.Join("testdata", "nodes.txt")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := subscription.ParseBody(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) < 1 {
		t.Fatal("expected nodes from testdata")
	}
}

func TestValidNode(t *testing.T) {
	cases := []struct {
		name string
		n    subscription.Node
		ok   bool
	}{
		{"ok", subscription.Node{Host: "a.com", Port: 443, Protocol: "vless"}, true},
		{"no host", subscription.Node{Port: 443, Protocol: "vless"}, false},
		{"no port", subscription.Node{Host: "a.com", Protocol: "vless"}, false},
		{"bad proto", subscription.Node{Host: "a.com", Port: 443, Protocol: "ftp"}, false},
		{"openvpn ok", subscription.Node{
			Host: "vpn.example.com", Port: 1194, Protocol: "openvpn",
			ConfigText: "client", CA: []string{"-----BEGIN CERTIFICATE-----"},
		}, true},
		{"openvpn no ca", subscription.Node{
			Host: "vpn.example.com", Port: 1194, Protocol: "openvpn", ConfigText: "client",
		}, false},
		{"openvpn no config", subscription.Node{
			Host: "vpn.example.com", Port: 1194, Protocol: "openvpn", CA: []string{"x"},
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := subscription.ValidNode(tc.n); got != tc.ok {
				t.Fatalf("ValidNode=%v want %v", got, tc.ok)
			}
		})
	}
}

func TestFindNodeByIDAndEndpoint(t *testing.T) {
	a := subscription.Node{ID: "id-a", Host: "h.example", Port: 443, Protocol: "vless"}
	b := subscription.Node{ID: "id-b", Host: "h.example", Port: 443, Protocol: "vless"}
	nodes := []subscription.Node{a, b}

	if n, ok := subscription.FindNodeByID(nodes, "id-b"); !ok || n.ID != "id-b" {
		t.Fatalf("by id: %+v %v", n, ok)
	}
	if _, ok := subscription.FindNodeByID(nodes, ""); ok {
		t.Fatal("empty id")
	}
	if n, ok := subscription.FindNodeByEndpoint(nodes, "H.EXAMPLE", 443, "VLESS"); !ok || n.ID != "id-a" {
		t.Fatalf("endpoint: %+v %v", n, ok)
	}
}

func TestFilterValidNodes(t *testing.T) {
	in := []subscription.Node{
		{Host: "a.com", Port: 1, Protocol: "vless"},
		{Host: "", Port: 1, Protocol: "vless"},
	}
	out := subscription.FilterValidNodes(in)
	if len(out) != 1 {
		t.Fatalf("%+v", out)
	}
}

func TestRemapSelection(t *testing.T) {
	prev := []subscription.Node{
		{ID: "old", Host: "n.example", Port: 443, Protocol: "vless"},
	}
	nextSameID := []subscription.Node{
		{ID: "old", Host: "n.example", Port: 443, Protocol: "vless"},
	}
	nextRemap := []subscription.Node{
		{ID: "new", Host: "n.example", Port: 443, Protocol: "vless"},
	}
	nextGone := []subscription.Node{
		{ID: "other", Host: "other.example", Port: 443, Protocol: "vless"},
	}

	id, counts := subscription.RemapSelection("old", map[string]int{"old": 3}, prev, nextSameID)
	if id != "old" || counts["old"] != 3 {
		t.Fatalf("keep: %s %v", id, counts)
	}

	id, counts = subscription.RemapSelection("old", map[string]int{"old": 3}, prev, nextRemap)
	if id != "new" || counts["new"] != 3 || counts["old"] != 0 {
		t.Fatalf("remap: %s %v", id, counts)
	}

	id, counts = subscription.RemapSelection("old", map[string]int{"old": 2}, prev, nextGone)
	if id != "other" {
		t.Fatalf("auto-select remaining: %s %v", id, counts)
	}
	if _, ok := counts["old"]; ok {
		t.Fatalf("clear counts: %v", counts)
	}

	id, counts = subscription.RemapSelection("", nil, prev, nextRemap)
	if id != "new" {
		t.Fatalf("auto-select single: %s %v", id, counts)
	}

	id, counts = subscription.RemapSelection("", nil, prev, []subscription.Node{
		{ID: "a", Host: "a.example", Port: 443, Protocol: "vless"},
		{ID: "b", Host: "b.example", Port: 443, Protocol: "vless"},
	})
	if id != "" || counts != nil {
		t.Fatalf("no auto-select for many: %s %v", id, counts)
	}
}
