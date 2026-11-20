package singbox

import (
	"testing"

	"github.com/Valden92/routebox/internal/nm"
)

func TestSnapshotSystemVPNStateFallback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     nm.SystemVPNStatus
		active     string
		wantUp     bool
		wantIface  string
		wantProbes bool
	}{
		{"connected", nm.SystemVPNStatus{Connected: true, Interface: "tun9"}, "", true, "tun9", false},
		{"connecting", nm.SystemVPNStatus{State: "connecting"}, "", true, "tun0", false},
		{"NM unavailable", nm.SystemVPNStatus{Error: "unavailable"}, "tun1", true, "tun1", true},
		{"known interface fallback", nm.SystemVPNStatus{Interface: "tun9"}, "tun9", true, "tun9", true},
		{"stale interface", nm.SystemVPNStatus{Interface: "tun9"}, "tun1", true, "tun1", true},
		{"offline", nm.SystemVPNStatus{}, "", false, "tun0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probes := 0
			up, iface := systemVPNStateFromStatus(tc.status, func(iface string) bool { probes++; return iface == tc.active })
			if up != tc.wantUp || iface != tc.wantIface || (probes > 0) != tc.wantProbes {
				t.Fatalf("up=%v iface=%s probes=%d", up, iface, probes)
			}
		})
	}
}
