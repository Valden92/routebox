import { describe, expect, it, vi } from "vitest";
import type { DomainRule } from "./api";
import {
  groupDomainRules,
  RulesDisclosure,
  rulesFilterKey,
  type RulesFilters,
} from "./rules-groups";
const rules: DomainRule[] = [
  { id: "1", pattern: "docs.example.org", path: "personal", source: "auto", enabled: true },
  { id: "2", pattern: "intranet.example.org", path: "work", source: "manual", enabled: true },
  { id: "3", pattern: "video.example.org", path: "personal", enabled: false },
  { id: "4", pattern: "news.example.org", path: "direct", source: "auto", enabled: true },
];
const all: RulesFilters = { query: "", path: "all", enabled: "all" };
function storage(initial: string | null = null) {
  let value = initial;
  return {
    getItem: vi.fn(() => value),
    setItem: vi.fn((_key: string, next: string) => {
      value = next;
    }),
  };
}
describe("rule groups", () => {
  it("separates sources and paths without reordering or changing saved rules", () => {
    const before = JSON.stringify(rules);
    const [auto, manual] = groupDomainRules(rules, all);
    expect(auto.total).toBe(2);
    expect(manual.total).toBe(2);
    expect(auto.groups.map((g) => g.key)).toEqual(["personal", "direct", "work"]);
    expect(manual.groups[0].rules.map((r) => r.id)).toEqual(["3"]);
    expect(manual.groups[2].rules.map((r) => r.id)).toEqual(["2"]);
    expect(JSON.stringify(rules)).toBe(before);
  });
  it("combines filters while preserving total counts", () => {
    const [auto, manual] = groupDomainRules(rules, {
      query: "  EXAMPLE.ORG  ",
      path: "personal",
      enabled: "disabled",
    });
    expect(auto.matched).toBe(0);
    expect(auto.total).toBe(2);
    expect(manual.matched).toBe(1);
    expect(manual.total).toBe(2);
    expect(manual.groups[0].rules.map((r) => r.id)).toEqual(["3"]);
    expect(manual.groups[0].total).toBe(1);
  });
  it("counts empty and zero-match groups", () => {
    expect(groupDomainRules([], all).every((s) => s.total === 0 && s.matched === 0)).toBe(true);
    expect(
      groupDomainRules(rules, { ...all, query: "missing" }).every((s) => s.matched === 0),
    ).toBe(true);
  });
  it("recognizes any active filter and normalizes repeated searches", () => {
    expect(rulesFilterKey(all)).toBe("");
    expect(rulesFilterKey({ ...all, query: "  " })).toBe("");
    expect(rulesFilterKey({ ...all, path: "work" })).not.toBe("");
    expect(rulesFilterKey({ ...all, enabled: "enabled" })).not.toBe("");
    expect(rulesFilterKey({ ...all, query: " EXAMPLE " })).toBe(
      rulesFilterKey({ ...all, query: "example" }),
    );
  });
});
describe("disclosure state", () => {
  it("initially expands sources and collapses path groups", () => {
    const state = new RulesDisclosure();
    expect(state.open("auto")).toBe(true);
    expect(state.open("manual")).toBe(true);
    expect(state.open("auto-personal")).toBe(false);
  });
  it("keeps independent choices across reloads", () => {
    const saved = storage();
    const state = new RulesDisclosure(saved);
    state.set("auto", false);
    state.set("manual-work", true);
    const restored = new RulesDisclosure(saved);
    expect(restored.open("auto")).toBe(false);
    expect(restored.open("manual")).toBe(true);
    expect(restored.open("manual-work")).toBe(true);
    expect(restored.open("manual-personal")).toBe(false);
  });
  it("reveals matches temporarily, preserves toggles on polls, restores layout on clear", () => {
    const saved = storage();
    const state = new RulesDisclosure(saved);
    state.set("auto", false);
    state.set("manual-work", true);
    saved.setItem.mockClear();
    state.setFilter("search");
    expect(state.open("auto")).toBe(true);
    expect(state.open("auto-personal")).toBe(true);
    state.set("auto-personal", false);
    state.setFilter("search");
    expect(state.open("auto-personal")).toBe(false);
    expect(saved.setItem).not.toHaveBeenCalled();
    state.setFilter("other");
    expect(state.open("auto-personal")).toBe(true);
    state.setFilter("");
    expect(state.open("auto")).toBe(false);
    expect(state.open("manual-work")).toBe(true);
  });
  it.each([
    null,
    "bad JSON",
    "[]",
    '{"version":2,"open":{"auto":false}}',
    '{"version":1,"open":null}',
  ])("uses defaults for invalid state: %s", (raw) => {
    expect(new RulesDisclosure(storage(raw)).open("auto")).toBe(true);
  });
  it("ignores unknown keys and nonboolean values", () => {
    const state = new RulesDisclosure(
      storage(
        '{"version":1,"open":{"auto":false,"manual":"false","auto-personal":true,"__proto__":false}}',
      ),
    );
    expect(state.open("auto")).toBe(false);
    expect(state.open("manual")).toBe(true);
    expect(state.open("auto-personal")).toBe(true);
  });
  it("works in memory when storage throws", () => {
    const state = new RulesDisclosure({
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("full");
      },
    });
    state.set("auto", false);
    expect(state.open("auto")).toBe(false);
  });
});
