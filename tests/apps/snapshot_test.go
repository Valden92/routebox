package apps_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Valden92/routebox/internal/apps"
)

const sockets = `tcp ESTAB 0 0 192.0.2.1:51000 203.0.113.9:443 users:(("Browser App",pid=20,fd=6))
tcp LISTEN 0 4096 [::1]:8080 [::]:* users:(("server",pid=10,fd=3),("worker",pid=11,fd=4))
udp UNCONN 0 0 0.0.0.0:5353 0.0.0.0:* users:(("Browser App",pid=20,fd=7),("Browser App",pid=20,fd=8))
udp ESTAB 0 0 [2001:db8::1]:54400 [2001:db8::2]:443 users:(("quic",pid=30,fd=5))
tcp TIME-WAIT 0 0 192.0.2.1:50000 203.0.113.7:443
`

func TestSnapshotRetainsProtocolsAddressesSharedAndUnknownOwners(t *testing.T) {
	calls := map[int]int{}
	list := apps.ParseSocketSnapshot(sockets, func(pid int) (string, string) {
		calls[pid]++
		return map[int]string{10: "server", 11: "worker", 20: "browser", 30: "quic"}[pid], "/fixture/app"
	})
	if len(list) != 5 {
		t.Fatalf("groups=%+v", list)
	}
	byPID := map[int]apps.NetworkApp{}
	for _, app := range list {
		byPID[app.PID] = app
	}
	if byPID[20].Connections != 2 || calls[20] != 1 {
		t.Fatal("duplicate FD became duplicate socket or repeated process lookup")
	}
	if byPID[10].Sockets[0].State != "LISTEN" || byPID[11].Sockets[0].LocalAddress != "[::1]:8080" {
		t.Fatal("shared listener lost")
	}
	if byPID[30].Sockets[0].Protocol != "udp" || byPID[30].Sockets[0].RemoteAddress != "[2001:db8::2]:443" {
		t.Fatal("UDP/IPv6 endpoint lost")
	}
	if byPID[0].Connections != 1 || byPID[0].ProcessName != "" || calls[0] != 0 || list[len(list)-1].PID != 0 {
		t.Fatal("unknown socket was hidden or assigned an owner")
	}
}
func TestSnapshotOrderIsStableAndEmptyListIsNotNil(t *testing.T) {
	info := func(pid int) (string, string) { return "same", "" }
	lines := strings.Split(strings.TrimSpace(sockets), "\n")
	forward := apps.ParseSocketSnapshot(strings.Join(lines, "\n"), info)
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	reverse := apps.ParseSocketSnapshot(strings.Join(lines, "\n"), info)
	if !reflect.DeepEqual(forward, reverse) {
		t.Fatal("scan order changes the result")
	}
	empty := apps.ParseSocketSnapshot("\ninvalid\ntcp ESTAB\nunix ESTAB 0 0 local peer", func(int) (string, string) { t.Fatal("unexpected lookup"); return "", "" })
	if empty == nil || len(empty) != 0 {
		t.Fatalf("bad empty response: %+v", empty)
	}
}
func TestSnapshotHandlesExitedProcessesAndMalformedPID(t *testing.T) {
	raw := `tcp ESTAB 0 0 127.0.0.1:10000 127.0.0.1:443 users:(("gone",pid=99,fd=3))
udp UNCONN 0 0 *:123 *:* users:(("bad",pid=9999999999999999999999,fd=3))`
	list := apps.ParseSocketSnapshot(raw, func(int) (string, string) { return "", "" })
	if len(list) != 2 || list[0].PID != 99 || list[0].ProcessName != "" || list[1].PID != 0 {
		t.Fatalf("exited/unknown process lost: %+v", list)
	}
}
