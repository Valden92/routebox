package singbox

import (
	"os/exec"
	"strings"
)

// WorkRoutesFromOS возвращает префиксы, которые система ведёт через tun0 (рабочий VPN).
func WorkRoutesFromOS(iface string) []string {
	if iface == "" {
		iface = "tun0"
	}
	out, err := exec.Command("ip", "route", "show", "dev", iface).CombinedOutput()
	if err != nil {
		out, _ = exec.Command("ip", "route", "show").CombinedOutput()
	}
	seen := make(map[string]bool)
	var routes []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		dest := fields[0]
		if dest == "default" || dest == iface {
			continue
		}
		if !strings.Contains(dest, "/") {
			dest += "/32"
		}
		if seen[dest] {
			continue
		}
		seen[dest] = true
		routes = append(routes, dest)
	}
	// Полные таблицы: маршруты via tun0
	full, _ := exec.Command("ip", "route", "show").CombinedOutput()
	for line := range strings.SplitSeq(string(full), "\n") {
		if !strings.Contains(line, "dev "+iface) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		dest := fields[0]
		if dest == "default" {
			continue
		}
		if !strings.Contains(dest, "/") {
			dest += "/32"
		}
		if seen[dest] {
			continue
		}
		seen[dest] = true
		routes = append(routes, dest)
	}
	return routes
}
