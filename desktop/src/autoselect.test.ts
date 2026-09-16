import { describe, expect, it } from "vitest";
import type { DomainRule } from "./api";
import {
  initialAutoSelectSitesText,
  parseSitesInput,
  renderAutoSelectProgress,
  sitesFromPersonalRules,
} from "./autoselect";

describe("parseSitesInput", () => {
  it("trims, prefixes https, dedupes, limits", () => {
    expect(parseSitesInput("cursor.com\nhttps://a.com\ncursor.com\n")).toEqual([
      "https://cursor.com",
      "https://a.com",
    ]);
  });
});

describe("sitesFromPersonalRules", () => {
  it("takes enabled personal host patterns", () => {
    const rules: DomainRule[] = [
      { id: "1", pattern: "cursor.com", path: "personal", enabled: true },
      { id: "2", pattern: "*.google.com", path: "personal", enabled: true },
      { id: "3", pattern: "corp.local", path: "work", enabled: true },
      { id: "4", pattern: "api2.cursor.sh", path: "personal", enabled: false },
    ];
    expect(sitesFromPersonalRules(rules)).toEqual(["https://cursor.com", "https://google.com"]);
  });
});

describe("initialAutoSelectSitesText", () => {
  it("falls back to defaults without storage/rules", () => {
    expect(initialAutoSelectSitesText([])).toContain("generate_204");
  });
});

describe("renderAutoSelectProgress", () => {
  it("shows checked/total", () => {
    const html = renderAutoSelectProgress({
      jobId: "j",
      subscriptionId: "s",
      phase: "probing",
      checked: 3,
      total: 40,
      currentName: "NL",
      attempts: [],
      finished: false,
    });
    expect(html).toContain("3");
    expect(html).toContain("40");
    expect(html).toContain("NL");
  });
});
