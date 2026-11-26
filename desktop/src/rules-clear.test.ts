import { describe, expect, it, vi } from "vitest";
import type { DomainRule } from "./api";
import { ruleClearSnapshot, ruleClearError, showRulesClearConfirmation } from "./rules-clear";
const rules: DomainRule[] = [
  { id: "auto", pattern: "auto.invalid", source: "auto", path: "personal", enabled: true },
  { id: "manual", pattern: "manual.invalid", source: "manual", path: "work", enabled: false },
  { id: "legacy", pattern: "legacy.invalid", path: "direct", enabled: true },
  { id: "import", pattern: "import.invalid", source: "import", path: "personal", enabled: true },
];
describe("clear rule scope", () => {
  it("captures every rule in the source, including disabled, legacy and imported rules", () => {
    expect(ruleClearSnapshot(rules, "auto")).toEqual(["auto"]);
    expect(ruleClearSnapshot(rules, "manual")).toEqual(["manual", "legacy", "import"]);
    const ids = ruleClearSnapshot(rules, "auto");
    rules.push({ ...rules[0], id: "new" });
    expect(ids).toEqual(["auto"]);
    rules.pop();
  });
  it("deduplicates IDs and handles empty sections", () => {
    expect(ruleClearSnapshot([rules[0], rules[0]], "auto")).toEqual(["auto"]);
    expect(ruleClearSnapshot([], "manual")).toEqual([]);
  });
  it("never opens or sends an empty confirmation", () => {
    const actions = { open: vi.fn(), close: vi.fn(), clear: vi.fn(), notify: vi.fn() };
    showRulesClearConfirmation("auto", [], actions);
    expect(actions.open).not.toHaveBeenCalled();
    expect(actions.clear).not.toHaveBeenCalled();
  });
  it("distinguishes persisted deletion from a failed save", () => {
    expect(ruleClearError({ code: "reapply_failed" })).toContain("удалены из настроек");
    expect(ruleClearError(new Error("write failed"))).toBe("write failed");
    expect(ruleClearError(null)).toBe("Не удалось очистить правила");
  });
});
