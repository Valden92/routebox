import type { DomainRule, RulesResponse, Subscription } from "./api";

export function filterRules(rules: DomainRule[], query: string): DomainRule[] {
  if (!query) return rules;
  const q = query.toLowerCase();
  return rules.filter((r) => r.pattern.toLowerCase().includes(q));
}

export function partitionDomainRules(rules: DomainRule[]): {
  auto: DomainRule[];
  manual: DomainRule[];
} {
  const auto: DomainRule[] = [];
  const manual: DomainRule[] = [];
  for (const r of rules) {
    if ((r.source || "manual") === "auto") auto.push(r);
    else manual.push(r);
  }
  return { auto, manual };
}

export function rulesDataSignature(data: RulesResponse): string {
  return JSON.stringify({
    domains: data.domains ?? [],
    apps: data.apps ?? [],
  });
}

export function formatRefreshLabel(sub: Subscription): string {
  if (!sub.autoRefresh) return "обновление вручную";
  const m = sub.refreshIntervalMinutes || 60;
  if (m < 60) return `авто каждые ${m} мин.`;
  if (m % 60 === 0) return `авто каждые ${m / 60} ч.`;
  return `авто каждые ${m} мин.`;
}
