package routing

import (
	"net"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// DomainRuleGroup is display metadata, never a routing pattern or priority.
func DomainRuleGroup(pattern string) string {
	host := strings.TrimSuffix(strings.TrimPrefix(NormalizeRulePattern(pattern), "*."), ".")
	if host == "" || net.ParseIP(host) != nil {
		return host
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return host
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(ascii)
	if err != nil {
		domain = ascii
	}
	display, err := idna.ToUnicode(domain)
	if err != nil {
		return domain
	}
	return display
}
