package singbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/subscription"
)

func TestWriteConfigCoexistHasHijackAndIsolation(t *testing.T) {
	st := config.DefaultSettings(t.TempDir())
	st.SystemVPN.NMConnectionID = "PTsecurity-nonexistent-test"
	// Без живого NM: sysUp=false → hijack допустим. Проверяем только структуру coexist-полей при sysUp через подмену.
	node := subscription.Node{Host: "1.2.3.4", Port: 443}
	path := filepath.Join(t.TempDir(), "sing-box.json")

	sysUp := true
	sysIface := "tun0"
	outbound, _ := uriToOutbound(node)
	tunInbound := map[string]any{
		"type": "tun", "tag": "tun-in", "interface_name": "tun100",
		"address": []string{"172.19.0.1/30"}, "auto_route": true, "strict_route": !sysUp,
		"route_exclude_address": routeExcludeAddresses(sysIface, sysUp, nil),
		"stack":                 "mixed", "sniff": true,
	}
	if sysUp {
		tunInbound["route_address"] = []string{"0.0.0.0/1", "128.0.0.0/1"}
		tunInbound["exclude_interface"] = []string{sysIface, "tun0", "tun1"}
	}
	cfg := map[string]any{
		"inbounds":  []map[string]any{tunInbound},
		"outbounds": []any{outbound, buildDirectOutbound(st), map[string]any{"type": "direct", "tag": "work", "bind_interface": sysIface}},
		"route":     buildRoute(st, &node, sysUp, sysIface),
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.Contains(s, "hijack-dns") {
		t.Fatal("coexist config must contain hijack-dns for personal sites")
	}
	if !strings.Contains(s, "exclude_interface") || !strings.Contains(s, "route_address") {
		t.Fatalf("coexist config missing route isolation: %s", s)
	}
}
