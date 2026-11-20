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
	for _, rule := range routing.CollapseDomainRules(enabledDomainRules(st.DomainRules)) {
		tag, available := pathTagIfAvailable(rule.Path, personalAvailable, workAvailable)
		if !available {
			continue
		}
		plan = append(plan, compiledDomainRule{pattern: rule.Pattern, outbound: tag, dns: "dns-" + tag})
	}
	return plan
}
