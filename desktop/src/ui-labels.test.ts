import { describe, expect, it } from "vitest";
import { escapeHtml, routerModeHint, routerModeLabel, routerPriorityLabel } from "./ui-labels";

describe("router labels", () => {
  it("labels modes", () => {
    expect(routerModeLabel("coexist")).toContain("системным");
    expect(routerModeLabel("full")).toBe("Обычный");
    expect(routerPriorityLabel("coexist")).toContain("Системный VPN");
    expect(routerModeHint()).toContain("выключен");
  });
});

describe("escapeHtml", () => {
  it("escapes markup", () => {
    expect(escapeHtml(`<a href="x">&`)).toBe("&lt;a href=&quot;x&quot;&gt;&amp;");
  });
});
