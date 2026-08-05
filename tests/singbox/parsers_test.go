package singbox_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/singbox"
)

func TestParseVPNRemoteHosts(t *testing.T) {
	data := `service-type = org.freedesktop.NetworkManager.openvpn, remote = 10.1.2.3:1194\, vpn.example.com:443\, edge.corp.example:1194, remote-cert-tls = server, reneg-seconds = 0`
	got := singbox.ParseVPNRemoteHosts(data)
	want := []string{"10.1.2.3", "vpn.example.com", "edge.corp.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	if singbox.ParseVPNRemoteHosts("") != nil {
		t.Fatal("empty")
	}
	if singbox.ParseVPNRemoteHosts("no-remote-here") != nil {
		t.Fatal("missing key")
	}
}

func TestParseResolvectlDNS(t *testing.T) {
	out := "Link 12 (tun0): 10.64.0.1 10.64.0.2\n"
	if got := singbox.ParseResolvectlDNS(out); got != "10.64.0.1" {
		t.Fatalf("%q", got)
	}
	if singbox.ParseResolvectlDNS("# comment\n") != "" {
		t.Fatal("comment only")
	}
}

func TestMergeRouteExclude(t *testing.T) {
	baseOnly := singbox.MergeRouteExclude(false, []string{"1.2.3.4/32"}, []string{"5.6.7.8/32"})
	if len(baseOnly) != 7 {
		t.Fatalf("base len %d: %v", len(baseOnly), baseOnly)
	}
	for _, c := range []string{"1.2.3.4/32", "5.6.7.8/32"} {
		for _, b := range baseOnly {
			if b == c {
				t.Fatalf("extra should not appear when include=false: %v", baseOnly)
			}
		}
	}

	merged := singbox.MergeRouteExclude(true, []string{"1.2.3.4/32", "10.0.0.0/8"}, []string{"1.2.3.4/32", "9.9.9.9/32"})
	seen := map[string]int{}
	for _, c := range merged {
		seen[c]++
		if seen[c] > 1 {
			t.Fatalf("duplicate %s in %v", c, merged)
		}
	}
	for _, need := range []string{"10.0.0.0/8", "1.2.3.4/32", "9.9.9.9/32", "192.168.0.0/16"} {
		if seen[need] != 1 {
			t.Fatalf("missing %s in %v", need, merged)
		}
	}
}

func TestParseSingBoxLogLine(t *testing.T) {
	got := singbox.ParseSingBoxLogLine(`ERROR[0000] listen tcp: bind: operation not permitted`)
	if !strings.Contains(got, "operation not permitted") {
		t.Fatalf("%q", got)
	}
}

func TestResolveErrorMessage(t *testing.T) {
	if singbox.ResolveErrorMessage("", "") != "" {
		t.Fatal("empty")
	}
	if msg := singbox.ResolveErrorMessage("exit status 1", ""); msg == "" || msg == "exit status 1" {
		t.Fatalf("generic: %q", msg)
	}
	if msg := singbox.ResolveErrorMessage("exit status 1", "operation not permitted"); msg == "" ||
		msg == "exit status 1" {
		t.Fatalf("perm hint: %q", msg)
	}
	if msg := singbox.ResolveErrorMessage("boom", "detail"); msg != "detail" {
		t.Fatalf("%q", msg)
	}
}
