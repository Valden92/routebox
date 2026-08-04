//go:build !linux

package singbox

import "github.com/dzaytsev/vpn-router/internal/config"

func systemVPNUp(st config.Settings) bool {
	return workVPNActive("tun0")
}

func SystemVPNUp(st config.Settings) bool {
	return systemVPNUp(st)
}

func SystemTunIface(st config.Settings) string {
	return "tun0"
}

func systemTunIface(st config.Settings) string {
	return "tun0"
}

func systemVPNDNS(string) string { return "" }

func systemVPNEndpointCIDRs(config.Settings) []string { return nil }
