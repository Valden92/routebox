import type { DomainRule } from "./api";

/** Missing metadata from an older daemon falls back to the exact host, not a guessed public suffix. */
export function groupRulesByDomain(rules: DomainRule[]) {
  const groups = new Map<string, DomainRule[]>();
  for (const rule of rules) {
    const domain =
      rule.domainGroup || rule.pattern.trim().toLowerCase().replace(/^\*\./, "").replace(/\.$/, "");
    const values = groups.get(domain) ?? [];
    values.push(rule);
    groups.set(domain, values);
  }
  return [...groups].map(([domain, values]) => ({ domain, rules: values }));
}
