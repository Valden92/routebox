package singbox

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/dzaytsev/vpn-router/internal/network"
)

const DefaultTunIface = "tun100"

var workRouteSnapshot struct {
	mu         sync.Mutex
	iface      string
	gateway    string
	hadDefault bool
}

func workVPNActive(iface string) bool {
	if iface == "" {
		iface = "tun0"
	}
	out, err := exec.Command("ip", "link", "show", iface).Output()
	if err != nil {
		return false
	}
	s := strings.ToLower(string(out))
	return strings.Contains(s, "up") || strings.Contains(s, "unknown")
}

// SnapshotWorkVPNRoutes запоминает default via tun0 до старта личного VPN (для восстановления после Stop).
func SnapshotWorkVPNRoutes(iface string) {
	if iface == "" {
		iface = "tun0"
	}
	if !workVPNActive(iface) {
		return
	}
	gw := defaultGatewayViaIface(iface)
	workRouteSnapshot.mu.Lock()
	defer workRouteSnapshot.mu.Unlock()
	workRouteSnapshot.iface = iface
	workRouteSnapshot.gateway = gw
	workRouteSnapshot.hadDefault = gw != ""
}

// routeExcludeAddresses — префиксы, которые sing-box не должен забирать в tun100 (рабочий VPN + LAN).
func routeExcludeAddresses(workIface string, includeWorkRoutes bool, extraCIDRs []string) []string {
	base := []string{
		"10.0.0.0/8",
		"127.0.0.0/8",
		"127.0.0.53/32",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"224.0.0.0/4",
	}
	if !includeWorkRoutes && !workVPNActive(workIface) {
		return base
	}
	seen := make(map[string]bool)
	var out []string
	add := func(cidr string) {
		if cidr == "" || seen[cidr] {
			return
		}
		seen[cidr] = true
		out = append(out, cidr)
	}
	for _, c := range base {
		add(c)
	}
	for _, c := range WorkRoutesFromOS(workIface) {
		add(c)
	}
	for _, c := range extraCIDRs {
		add(c)
	}
	return out
}

// RestoreAfterPersonalVPN убирает маршруты tun100 после sing-box (без sudo / polkit).
func RestoreAfterPersonalVPN() {
	RestoreAfterPersonalVPNOn(network.DefaultMainIface())
}

func RestoreAfterPersonalVPNOn(mainIface string) {
	if mainIface == "" {
		mainIface = network.DefaultMainIface()
	}
	workIface := "tun0"
	// Даём sing-box самому снять маршруты/TUN
	time.Sleep(800 * time.Millisecond)
	removePersonalVPNRoutes(DefaultTunIface)

	workActive := workVPNActive(workIface)
	if workActive {
		restoreWorkVPNDefault(workIface)
		if defaultGatewayViaIface(workIface) != "" {
			return
		}
		if hasUsableDefault(DefaultTunIface) {
			return
		}
		ensureDefaultViaMain(mainIface)
		return
	}
	if hasUsableDefault(DefaultTunIface) {
		return
	}
	ensureDefaultViaMain(mainIface)
}

func removePersonalVPNRoutes(tunIface string) {
	out, _ := exec.Command("ip", "-4", "route", "show").CombinedOutput()
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "dev "+tunIface) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		dest := fields[0]
		switch dest {
		case "default":
			_ = exec.Command("ip", "route", "del", "default", "dev", tunIface).Run()
		case "0.0.0.0/1", "128.0.0.0/1":
			_ = exec.Command("ip", "route", "del", dest, "dev", tunIface).Run()
		}
	}
}

func defaultGatewayViaIface(iface string) string {
	out, err := exec.Command("ip", "-4", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "dev "+iface) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "default" && fields[1] == "via" {
			return fields[2]
		}
	}
	return ""
}

func restoreWorkVPNDefault(iface string) {
	if gw := defaultGatewayViaIface(iface); gw != "" {
		_ = exec.Command("ip", "route", "replace", "default", "via", gw, "dev", iface).Run()
		return
	}
	workRouteSnapshot.mu.Lock()
	gw, snapIface, had := workRouteSnapshot.gateway, workRouteSnapshot.iface, workRouteSnapshot.hadDefault
	workRouteSnapshot.mu.Unlock()
	if !had || gw == "" {
		return
	}
	if snapIface == "" {
		snapIface = iface
	}
	_ = exec.Command("ip", "route", "replace", "default", "via", gw, "dev", snapIface).Run()
}

// hasUsableDefault — есть default не через excludeDev (обычно tun100).
func hasUsableDefault(excludeDev string) bool {
	out, err := exec.Command("ip", "-4", "route", "show", "default").Output()
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "default") {
			continue
		}
		if excludeDev != "" && strings.Contains(line, "dev "+excludeDev) {
			continue
		}
		return true
	}
	return false
}

func ensureDefaultViaMain(iface string) {
	gw := gatewayForIface(iface)
	if gw == "" {
		return
	}
	_ = exec.Command("ip", "route", "replace", "default", "via", gw, "dev", iface, "metric", "100").Run()
}

func gatewayForIface(iface string) string {
	out, err := exec.Command("ip", "-4", "route", "show", "dev", iface).Output()
	if err == nil {
		for line := range strings.SplitSeq(string(out), "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) >= 3 && fields[0] == "default" && fields[1] == "via" {
				return fields[2]
			}
		}
	}
	if out, err := exec.Command("ip", "-4", "route", "show", "default").Output(); err == nil {
		for line := range strings.SplitSeq(string(out), "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			for i, f := range fields {
				if f == "dev" && i+1 < len(fields) && fields[i+1] == iface && i >= 2 && fields[i-2] == "via" {
					return fields[i-1]
				}
			}
		}
	}
	return ""
}

func HasTUNCapability(bin string) bool {
	path := ResolveBin(bin)
	out, err := exec.Command("getcap", path).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "cap_net_admin")
}

func RequireTUNCapability(bin string) error {
	path := ResolveBin(bin)
	if HasTUNCapability(bin) {
		return nil
	}
	return fmt.Errorf("выполните: make sync (бинарник: %s)", path)
}
