package singbox

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/nm"
)

// SystemVPNUp — системный VPN активен или подключается (NM).
func SystemVPNUp(st config.Settings) bool {
	return systemVPNUp(st)
}

func systemVPNUp(st config.Settings) bool {
	id := st.SystemVPN.NMConnectionID
	if id == "" {
		id = "PTsecurity"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s := nm.Status(ctx, id)
	if s.Connected || s.State == "connecting" {
		return true
	}
	for _, iface := range []string{s.Interface, "tun0", "tun1"} {
		if iface != "" && workVPNActive(iface) {
			return true
		}
	}
	return false
}

// SystemTunIface — интерфейс системного VPN (tun0 и т.д.).
func SystemTunIface(st config.Settings) string {
	return systemTunIface(st)
}

func systemTunIface(st config.Settings) string {
	id := st.SystemVPN.NMConnectionID
	if id == "" {
		return "tun0"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s := nm.Status(ctx, id)
	if s.Interface != "" {
		return s.Interface
	}
	return "tun0"
}

// systemVPNDNS — резолвер на интерфейсе системного VPN (для dns-work).
func systemVPNDNS(iface string) string {
	if iface == "" {
		iface = "tun0"
	}
	out, err := exec.Command("resolvectl", "dns", iface).Output()
	if err != nil {
		return ""
	}
	return ParseResolvectlDNS(string(out))
}

func systemVPNEndpointCIDRs(st config.Settings) []string {
	id := st.SystemVPN.NMConnectionID
	if id == "" {
		id = "PTsecurity"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, err := nm.Run(ctx, "-g", "vpn.data", "connection", "show", id)
	if err != nil || strings.TrimSpace(data) == "" {
		return nil
	}
	hosts := ParseVPNRemoteHosts(data)
	seen := map[string]struct{}{}
	var cidrs []string
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			cidr := ip.String() + "/32"
			if _, ok := seen[cidr]; !ok {
				seen[cidr] = struct{}{}
				cidrs = append(cidrs, cidr)
			}
			continue
		}
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			continue
		}
		for _, raw := range ips {
			ip := net.ParseIP(raw)
			if ip == nil || ip.To4() == nil {
				continue
			}
			cidr := ip.String() + "/32"
			if _, ok := seen[cidr]; ok {
				continue
			}
			seen[cidr] = struct{}{}
			cidrs = append(cidrs, cidr)
		}
	}
	return cidrs
}

// ParseVPNRemoteHosts извлекает host'ы из nmcli vpn.data (ключ remote = …).
func ParseVPNRemoteHosts(data string) []string {
	const key = "remote = "
	start := strings.Index(data, key)
	if start < 0 {
		return nil
	}
	remote := data[start+len(key):]
	for _, marker := range []string{", remote-cert-tls =", ", remote-random =", ", reneg-seconds =", ", tls-"} {
		if idx := strings.Index(remote, marker); idx >= 0 {
			remote = remote[:idx]
		}
	}
	remote = strings.ReplaceAll(remote, `\\,`, `\,`)
	parts := strings.Split(remote, `\,`)
	var hosts []string
	for _, part := range parts {
		part = strings.TrimSpace(strings.ReplaceAll(part, `\:`, ":"))
		if part == "" {
			continue
		}
		if idx := strings.Index(part, ":"); idx >= 0 {
			part = part[:idx]
		}
		part = strings.Trim(part, "[] ")
		if part != "" {
			hosts = append(hosts, part)
		}
	}
	return hosts
}

// ParseResolvectlDNS берёт первый IPv4 из вывода `resolvectl dns <iface>`.
func ParseResolvectlDNS(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		for _, f := range fields {
			if strings.Count(f, ".") == 3 && !strings.Contains(f, ":") {
				return f
			}
		}
	}
	return ""
}
