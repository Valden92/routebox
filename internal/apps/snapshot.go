package apps

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Socket struct {
	Protocol      string `json:"protocol"`
	State         string `json:"state"`
	LocalAddress  string `json:"localAddress"`
	RemoteAddress string `json:"remoteAddress"`
}

type NetworkApp struct {
	PID         int      `json:"pid"`
	ProcessName string   `json:"processName"`
	ExecPath    string   `json:"execPath,omitempty"`
	Connections int      `json:"connections"` // socket count; retained for API compatibility
	Sockets     []Socket `json:"sockets"`
}

var socketPID = regexp.MustCompile(`,pid=([0-9]+),`)

// ParseSocketSnapshot reads ss -H -O -n -t -u -a -p, without querying the host.
// A socket shared by processes is listed for each owner; absent owners use PID 0.
func ParseSocketSnapshot(raw string, processInfo func(int) (string, string)) []NetworkApp {
	byPID := map[int]*NetworkApp{}
	for line := range strings.SplitSeq(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || (fields[0] != "tcp" && fields[0] != "udp") {
			continue
		}
		socket := Socket{Protocol: fields[0], State: fields[1], LocalAddress: fields[4], RemoteAddress: fields[5]}
		owners := map[int]bool{}
		for _, match := range socketPID.FindAllStringSubmatch(strings.Join(fields[6:], " "), -1) {
			pid, err := strconv.Atoi(match[1])
			if err == nil && pid > 0 {
				owners[pid] = true
			}
		}
		if len(owners) == 0 {
			owners[0] = true
		}
		for pid := range owners {
			app := byPID[pid]
			if app == nil {
				app = &NetworkApp{PID: pid}
				if pid > 0 {
					app.ProcessName, app.ExecPath = processInfo(pid)
				}
				byPID[pid] = app
			}
			app.Sockets = append(app.Sockets, socket)
			app.Connections++
		}
	}
	list := make([]NetworkApp, 0, len(byPID))
	for _, app := range byPID {
		sort.SliceStable(app.Sockets, func(i, j int) bool {
			a, b := app.Sockets[i], app.Sockets[j]
			return a.Protocol+"\x00"+a.LocalAddress+"\x00"+a.RemoteAddress+"\x00"+a.State < b.Protocol+"\x00"+b.LocalAddress+"\x00"+b.RemoteAddress+"\x00"+b.State
		})
		list = append(list, *app)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].PID == 0 || list[j].PID == 0 {
			return list[i].PID != 0
		}
		a, b := strings.ToLower(list[i].ProcessName), strings.ToLower(list[j].ProcessName)
		if a != b {
			return a < b
		}
		return list[i].PID < list[j].PID
	})
	return list
}
