package singbox

import (
	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/routing"
)

type compiledDomainRule struct {
	pattern  string
	outbound string
	dns      string
}

// DNS and traffic must retain the same explicit exceptions and rule ordering.
// Resolve precedence before dropping unavailable paths, as traffic routing did before.
func compileDomainRules(st config.Settings, personalAvailable, workAvailable bool) []compiledDomainRule {
	var plan []compiledDomainRule
	var allowed []config.DomainRule
	for _, rule := range enabledDomainRules(st.DomainRules) {
		if rule.Path == config.RouteBlock {
			pattern, err := routing.NormalizeBlockPattern(rule.Pattern)
			if err == nil {
				plan = append(plan, compiledDomainRule{pattern: pattern, outbound: "block"})
			}
		} else {
			allowed = append(allowed, rule)
		}
	}
	for _, rule := range routing.CollapseDomainRules(allowed) {
		tag, available := pathTagIfAvailable(rule.Path, personalAvailable, workAvailable)
		if !available {
			continue
		}
		plan = append(plan, compiledDomainRule{pattern: rule.Pattern, outbound: tag, dns: "dns-" + tag})
	}
	return plan
}
