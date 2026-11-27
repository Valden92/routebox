package routing

import (
	"fmt"
	"net"
	"strings"

	"golang.org/x/net/idna"

	"github.com/Valden92/routebox/internal/config"
)

// NormalizeBlockPattern accepts a domain or suffix, not an address or global wildcard.
func NormalizeBlockPattern(raw string) (string, error) {
	pattern := NormalizeRulePattern(raw)
	host := strings.TrimSuffix(strings.TrimPrefix(pattern, "*."), ".")
	ascii, err := idna.Lookup.ToASCII(host)
	valid := err == nil && ascii != "" && len(ascii) <= 253 && net.ParseIP(ascii) == nil && !strings.ContainsAny(ascii, "* /:")
	for _, label := range strings.Split(ascii, ".") {
		valid = valid && len(label) > 0 && len(label) <= 63
	}
	if !valid {
		return "", fmt.Errorf("для запрета укажите домен или *.example.com, без IP-адреса")
	}
	if strings.HasPrefix(pattern, "*.") {
		return "*." + ascii, nil
	}
	return ascii, nil
}

// BlockingDomainRule is independent of routing/learning precedence: enabled deny wins.
func BlockingDomainRule(rules []config.DomainRule, host string) (config.DomainRule, bool) {
	host, err := NormalizeBlockPattern(host)
	if err != nil {
		return config.DomainRule{}, false
	}
	for _, rule := range rules {
		if !rule.Enabled || rule.Path != config.RouteBlock {
			continue
		}
		pattern, err := NormalizeBlockPattern(rule.Pattern)
		if err != nil {
			continue
		}
		if pattern == host || strings.HasPrefix(pattern, "*.") && (host == pattern[2:] || strings.HasSuffix(host, pattern[1:])) {
			return rule, true
		}
	}
	return config.DomainRule{}, false
}
