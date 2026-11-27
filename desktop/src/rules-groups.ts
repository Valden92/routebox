import type { DomainRule, RoutePath } from "./api";
import { filterRules, partitionDomainRules } from "./rules-util";

export const ruleSources = [
  { key: "auto", title: "Автоматические правила" },
  { key: "manual", title: "Ручные правила" },
] as const;
export const rulePaths = [
  { key: "personal", title: "Личный VPN" },
  { key: "direct", title: "Без VPN" },
  { key: "work", title: "Системный VPN" },
] as const;
export type RuleSource = (typeof ruleSources)[number]["key"];
export type RuleGroupKey = RuleSource | `${RuleSource}-${RoutePath}`;
export interface RulesFilters {
  query: string;
  path: RoutePath | "all";
  enabled: "all" | "enabled" | "disabled";
}

export function rulesFilterKey(filters: RulesFilters): string {
  const query = filters.query.trim().toLowerCase();
  return query || filters.path !== "all" || filters.enabled !== "all"
    ? JSON.stringify([query, filters.path, filters.enabled])
    : "";
}

export function groupDomainRules(domains: DomainRule[], filters: RulesFilters) {
  const all = partitionDomainRules(domains);
  const matching = partitionDomainRules(
    filterRules(domains, filters.query.trim()).filter(
      (rule) =>
        (filters.path === "all" || filters.path === rule.path) &&
        (filters.enabled === "all" || rule.enabled === (filters.enabled === "enabled")),
    ),
  );
  return ruleSources.map((source) => ({
    ...source,
    total: all[source.key].length,
    matched: matching[source.key].length,
    groups: rulePaths.map((path) => ({
      ...path,
      total: all[source.key].filter((rule) => rule.path === path.key).length,
      rules: matching[source.key].filter((rule) => rule.path === path.key),
    })),
  }));
}
export type RulesGroups = ReturnType<typeof groupDomainRules>;

const storageKey = "routebox.rules.disclosure.v1";
const validKeys: RuleGroupKey[] = ruleSources.flatMap(({ key }) => [
  key,
  ...rulePaths.map((path): RuleGroupKey => `${key}-${path.key}`),
]);
type DisclosureValues = Partial<Record<RuleGroupKey, boolean>>;
type DisclosureStorage = Pick<Storage, "getItem" | "setItem">;

/** Filters temporarily reveal matches without overwriting the user's layout. */
export class RulesDisclosure {
  private saved: DisclosureValues = {};
  private temporary: DisclosureValues = {};
  private filterKey = "";

  constructor(private readonly storage?: DisclosureStorage) {
    try {
      const data: unknown = JSON.parse(storage?.getItem(storageKey) ?? "null");
      if (!data || typeof data !== "object" || !("version" in data) || data.version !== 1) return;
      const values = "open" in data ? data.open : null;
      if (!values || typeof values !== "object" || Array.isArray(values)) return;
      const entries = values as Record<string, unknown>;
      for (const key of validKeys) {
        const value = entries[key];
        if (Object.prototype.hasOwnProperty.call(entries, key) && typeof value === "boolean") {
          this.saved[key] = value;
        }
      }
    } catch {
      // Unavailable storage or old/corrupt data must not hide the controls.
    }
  }

  setFilter(key: string) {
    if (key !== this.filterKey) this.temporary = {};
    this.filterKey = key;
  }

  open(key: RuleGroupKey): boolean {
    if (this.filterKey) return this.temporary[key] ?? true;
    return this.saved[key] ?? !key.includes("-");
  }

  set(key: RuleGroupKey, open: boolean) {
    if (this.filterKey) {
      this.temporary[key] = open;
      return;
    }
    this.saved[key] = open;
    try {
      this.storage?.setItem(storageKey, JSON.stringify({ version: 1, open: this.saved }));
    } catch {
      // Keep the chosen layout in memory when persistent storage is unavailable.
    }
  }
}
