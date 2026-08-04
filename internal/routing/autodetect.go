package routing

import (
	"net"
	"strings"

	"github.com/Valden92/routebox/internal/config"
)

// SuggestPath returns a routing path using heuristics; user rules override elsewhere.
func SuggestPath(host string, workRoutes []string, corpSuffixes []string) config.RoutePath {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return config.RouteDirect
	}
	if isPrivateHost(host) {
		return config.RouteDirect
	}
	for _, s := range corpSuffixes {
		if s != "" && strings.HasSuffix(host, strings.ToLower(s)) {
			return config.RouteWork
		}
	}
	if matchWorkRoutes(host, workRoutes) {
		return config.RouteWork
	}
	// common direct candidates in RU — user can override
	if strings.HasSuffix(host, ".ru") || strings.HasSuffix(host, ".рф") {
		return config.RouteDirect
	}
	return config.RoutePersonal
}

func isPrivateHost(host string) bool {
	if strings.HasSuffix(host, ".local") || host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return false
}

func matchWorkRoutes(host string, routes []string) bool {
	ips, err := net.LookupHost(host)
	if err != nil {
		return false
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		for _, r := range routes {
			_, cidr, err := net.ParseCIDR(strings.Fields(r)[0])
			if err != nil {
				if n := net.ParseIP(strings.Fields(r)[0]); n != nil && n.Equal(ip) {
					return true
				}
				continue
			}
			if cidr.Contains(ip) {
				return true
			}
		}
	}
	return false
}

func DefaultCorpSuffixes() []string {
	return []string{".ptsecurity.ru", ".ptsecurity.com"}
}
