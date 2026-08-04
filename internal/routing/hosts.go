package routing

import (
	"net"
	"net/url"
	"strings"

	"github.com/Valden92/routebox/internal/config"
)

// IsCorpHost — корпоративный хост по DefaultCorpSuffixes (без HTTP-проб / MFA).
func IsCorpHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, suffix := range DefaultCorpSuffixes() {
		if suffix != "" && strings.HasSuffix(host, strings.ToLower(suffix)) {
			return true
		}
	}
	return false
}

// NormalizeRulePattern извлекает hostname из URL или сырого паттерна.
func NormalizeRulePattern(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "*.") {
		return raw
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		host = strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
		if i := strings.IndexAny(host, "/:"); i >= 0 {
			host = host[:i]
		}
	}
	return strings.TrimSpace(host)
}

// NormalizeObservedHost — хост из live traffic / history, без IP и local/arpa.
func NormalizeObservedHost(raw string) string {
	host := NormalizeRulePattern(raw)
	if host == "" || !strings.Contains(host, ".") {
		return ""
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".arpa") {
		return ""
	}
	if net.ParseIP(host) != nil {
		return ""
	}
	return host
}

// UniqueHosts дедуплицирует и нормализует список хостов.
func UniqueHosts(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, h := range in {
		h = NormalizeObservedHost(h)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

// FindDomainRuleIndex ищет правило по точному host или wildcard *.suffix.
func FindDomainRuleIndex(rules []config.DomainRule, host string) int {
	host = strings.ToLower(strings.TrimSpace(host))
	for i, r := range rules {
		pattern := strings.ToLower(strings.TrimSpace(r.Pattern))
		if pattern == host {
			return i
		}
		if strings.HasPrefix(pattern, "*.") && strings.HasSuffix(host, strings.TrimPrefix(pattern, "*")) {
			return i
		}
	}
	return -1
}

// FilterDomainRules удаляет правило с id.
func FilterDomainRules(rules []config.DomainRule, id string) []config.DomainRule {
	var out []config.DomainRule
	for _, r := range rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	return out
}

// FilterAppRules удаляет правило с id.
func FilterAppRules(rules []config.AppRule, id string) []config.AppRule {
	var out []config.AppRule
	for _, r := range rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	return out
}
