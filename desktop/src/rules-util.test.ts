import { describe, expect, it } from "vitest";
import type { DomainRule, Subscription } from "./api";
import {
  filterRules,
  formatRefreshLabel,
  partitionDomainRules,
  rulesDataSignature,
} from "./rules-util";

const rules: DomainRule[] = [
  { id: "1", pattern: "github.com", path: "personal", source: "auto", enabled: true },
  { id: "2", pattern: "intranet.corp", path: "work", source: "manual", enabled: true },
  { id: "3", pattern: "GitLab.example", path: "personal", enabled: false },
];

describe("filterRules", () => {
  it("filters case-insensitively", () => {
    expect(filterRules(rules, "").map((r) => r.id)).toEqual(["1", "2", "3"]);
    expect(filterRules(rules, "git").map((r) => r.id)).toEqual(["1", "3"]);
    expect(filterRules(rules, "CORP").map((r) => r.id)).toEqual(["2"]);
  });
});

describe("partitionDomainRules", () => {
  it("splits auto vs manual (default manual)", () => {
    const { auto, manual } = partitionDomainRules(rules);
    expect(auto.map((r) => r.id)).toEqual(["1"]);
    expect(manual.map((r) => r.id)).toEqual(["2", "3"]);
  });
});

describe("rulesDataSignature", () => {
  it("stable for same payload", () => {
    const a = rulesDataSignature({ domains: rules, apps: [] });
    const b = rulesDataSignature({ domains: rules, apps: [] });
    expect(a).toBe(b);
    expect(rulesDataSignature({ domains: [], apps: [] })).not.toBe(a);
  });
});

describe("formatRefreshLabel", () => {
  const base: Subscription = {
    id: "s",
    name: "sub",
    url: "https://example.com",
    refreshIntervalMinutes: 60,
    autoRefresh: false,
    enabled: true,
  };

  it("formats intervals", () => {
    expect(formatRefreshLabel(base)).toBe("обновление вручную");
    expect(formatRefreshLabel({ ...base, autoRefresh: true, refreshIntervalMinutes: 30 })).toBe(
      "авто каждые 30 мин.",
    );
    expect(formatRefreshLabel({ ...base, autoRefresh: true, refreshIntervalMinutes: 120 })).toBe(
      "авто каждые 2 ч.",
    );
    expect(formatRefreshLabel({ ...base, autoRefresh: true, refreshIntervalMinutes: 90 })).toBe(
      "авто каждые 90 мин.",
    );
    expect(formatRefreshLabel({ ...base, autoRefresh: true, refreshIntervalMinutes: 0 })).toBe(
      "авто каждые 1 ч.",
    );
  });
});
