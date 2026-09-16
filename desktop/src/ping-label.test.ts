import { describe, expect, it } from "vitest";
import { formatPing, pingLevel, pingLevelLabel } from "./ping-label";

describe("pingLevel", () => {
  it("maps thresholds", () => {
    expect(pingLevel(0)).toBe("low");
    expect(pingLevel(79)).toBe("low");
    expect(pingLevel(80)).toBe("medium");
    expect(pingLevel(149)).toBe("medium");
    expect(pingLevel(150)).toBe("high");
    expect(pingLevel(299)).toBe("high");
    expect(pingLevel(300)).toBe("very_high");
  });
});

describe("formatPing", () => {
  it("formats with label", () => {
    expect(formatPing(42)).toBe("42 ms (низкий)");
    expect(formatPing(42, "tcp")).toBe("42 ms (низкий, tcp)");
    expect(formatPing(42, "icmp")).toBe("42 ms (низкий, icmp)");
    expect(formatPing(null)).toBe("—");
    expect(formatPing(undefined)).toBe("—");
    expect(formatPing(Number.NaN)).toBe("—");
  });

  it("exposes Russian labels", () => {
    expect(pingLevelLabel(200)).toBe("высокий");
  });
});
