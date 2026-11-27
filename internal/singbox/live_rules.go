package singbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/subscription"
)

var liveRulePaths = []string{"block", "direct", "proxy", "work"}

// LiveRuleFiles compiles disjoint matches so grouping by outbound does not
// change manual/automatic, exact/suffix, or first application rule precedence.
func LiveRuleFiles(st config.Settings, personal, work bool) (map[string][]byte, error) {
	domains := map[string][]map[string]any{}
	apps := map[string][]map[string]any{}
	plan := compileDomainRules(st, personal, work)
	for i, rule := range plan {
		var excluded []map[string]any
		shadowed := false
		for _, earlier := range plan[:i] {
			if earlier.outbound == rule.outbound {
				continue
			}
			if patternContains(earlier.pattern, rule.pattern) {
				shadowed = true
				break
			}
			if patternContains(rule.pattern, earlier.pattern) {
				excluded = append(excluded, domainMatchRule(earlier.pattern, nil))
			}
		}
		if shadowed {
			continue
		}
		match := domainMatchRule(rule.pattern, nil)
		if len(excluded) > 0 {
			match = map[string]any{"type": "logical", "mode": "and", "rules": []map[string]any{
				match, {"type": "logical", "mode": "or", "rules": excluded, "invert": true},
			}}
		}
		domains[rule.outbound] = append(domains[rule.outbound], match)
	}
	seen := map[string]bool{}
	for _, app := range st.AppRules {
		tag, available := pathTagIfAvailable(app.Path, personal, work)
		if !app.Enabled || app.ProcessName == "" || !available || seen[app.ProcessName] {
			continue
		}
		seen[app.ProcessName] = true
		apps[tag] = append(apps[tag], map[string]any{"process_name": []string{app.ProcessName}})
	}
	files := map[string][]byte{}
	for _, path := range liveRulePaths {
		// Keep process lookup enabled from startup, including when there are no app
		// rules yet. This conjunction never matches any process (P AND NOT P).
		never := map[string]any{"type": "logical", "mode": "and", "rules": []map[string]any{
			{"process_name": []string{"routebox-metadata"}},
			{"process_name": []string{"routebox-metadata"}, "invert": true},
		}}
		for kind, rules := range map[string][]map[string]any{"domains": domains[path], "apps": append([]map[string]any{never}, apps[path]...)} {
			if path == "block" && kind == "apps" {
				continue
			}
			if rules == nil {
				rules = []map[string]any{}
			}
			body, err := json.MarshalIndent(map[string]any{"version": 3, "rules": rules}, "", "  ")
			if err != nil {
				return nil, err
			}
			files[path+"-"+kind+".json"] = body
		}
	}
	return files, nil
}

// A bare domain is exact; a wildcard follows sing-box domain_suffix semantics,
// including the apex itself. Domain normalization happens when rules are saved.
func patternContains(parent, child string) bool {
	parent, child = strings.ToLower(parent), strings.ToLower(child)
	if !strings.HasPrefix(parent, "*.") {
		return parent == child
	}
	p := strings.TrimPrefix(parent, "*.")
	c := strings.TrimPrefix(child, "*.")
	return c == p || strings.HasSuffix(c, "."+p)
}

// WriteLiveRouterConfig keeps transport, TUN, coexist and fallback rules static.
// Rule files must already exist; preparation always uses a private new directory.
func WriteLiveRouterConfig(path string, node *subscription.Node, st config.Settings, tun string, snapshot RouterNetworkSnapshot, ruleDir string) error {
	base := st
	base.DomainRules, base.AppRules = nil, nil
	if err := WriteRouterConfigWithSnapshot(path, node, base, tun, snapshot); err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		return err
	}
	route := cfg["route"].(map[string]any)
	dns := cfg["dns"].(map[string]any)
	var sets, domainRoutes, appRoutes, dnsRules, blockRoutes []any
	personal := st.PersonalVPN.Enabled && node != nil
	for _, tag := range liveRulePaths {
		if tag == "proxy" && !personal || tag == "work" && !snapshot.SystemVPNUp {
			continue
		}
		for _, kind := range []string{"domains", "apps"} {
			if tag == "block" && kind == "apps" {
				continue
			}
			name := tag + "-" + kind
			sets = append(sets, map[string]any{"type": "local", "tag": name, "format": "source", "path": filepath.Join(ruleDir, name+".json")})
		}
		if tag == "block" {
			blockRoutes = append(blockRoutes, map[string]any{"rule_set": []string{"block-domains"}, "action": "reject"})
			dnsRules = append(dnsRules, map[string]any{"rule_set": []string{"block-domains"}, "action": "reject"})
			continue
		}
		domainRoutes = append(domainRoutes, map[string]any{"rule_set": []string{tag + "-domains"}, "outbound": tag})
		appRoutes = append(appRoutes, map[string]any{"rule_set": []string{tag + "-apps"}, "outbound": tag})
		dnsRules = append(dnsRules, map[string]any{"rule_set": []string{tag + "-domains"}, "server": "dns-" + tag})
	}
	rules := route["rules"].([]any)
	// Deny after sniff/DNS hijack, before loopback, probe, endpoint and work bypasses.
	ordered := append([]any{}, rules[:2]...)
	ordered = append(ordered, blockRoutes...)
	rules = append(ordered, rules[2:]...)
	// The final private-IP fallback stays after explicit domains and apps.
	fallback := rules[len(rules)-1]
	rules = append(rules[:len(rules)-1], domainRoutes...)
	rules = append(rules, appRoutes...)
	rules = append(rules, fallback)
	route["rules"], route["rule_set"] = rules, sets
	dns["rules"] = append(dnsRules, dns["rules"].([]any)...)
	return writeJSON(path, cfg)
}
