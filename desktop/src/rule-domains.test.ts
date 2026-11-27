import { describe, expect, it } from "vitest";
import type { DomainRule } from "./api";
import { groupRulesByDomain } from "./rule-domains";
const rules: DomainRule[] = [
  {
    id: "1",
    pattern: "a.example.co.uk",
    domainGroup: "example.co.uk",
    path: "direct",
    source: "auto",
    enabled: true,
  },
  {
    id: "2",
    pattern: "other.test",
    domainGroup: "other.test",
    path: "direct",
    source: "auto",
    enabled: true,
  },
  {
    id: "3",
    pattern: "*.example.co.uk",
    domainGroup: "example.co.uk",
    path: "direct",
    source: "auto",
    enabled: false,
  },
];
describe("domain families", () => {
  it("groups by server-provided domain while preserving each rule and stable order", () => {
    const before = JSON.stringify(rules);
    const groups = groupRulesByDomain(rules);
    expect(groups.map((g) => g.domain)).toEqual(["example.co.uk", "other.test"]);
    expect(groups[0].rules).toEqual([rules[0], rules[2]]);
    expect(groups[0].rules[0]).toBe(rules[0]);
    expect(JSON.stringify(rules)).toBe(before);
  });
  it("uses exact hosts when old API has no grouping metadata", () => {
    const values = rules.map(({ domainGroup: _ignored, ...rule }) => rule);
    expect(groupRulesByDomain(values).map((g) => g.domain)).toEqual([
      "a.example.co.uk",
      "other.test",
      "example.co.uk",
    ]);
  });
  it("groups equivalent exact host and wildcard without losing either", () => {
    expect(
      groupRulesByDomain([
        { ...rules[0], pattern: "*.EXAMPLE.COM", domainGroup: undefined },
        { ...rules[2], pattern: "example.com.", domainGroup: undefined },
      ])[0].rules,
    ).toHaveLength(2);
    expect(groupRulesByDomain([])).toEqual([]);
  });
});
