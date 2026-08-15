import { describe, expect, it } from "vitest";
import type { DomainRule, Subscription } from "./api";
import {
  decodeHeaderText,
  filterRules,
  formatAnnounce,
  formatRefreshLabel,
  formatServerCount,
  formatSubscriptionDates,
  formatSubscriptionQuota,
  formatSubscriptionStatusLine,
  formatSubscriptionType,
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

describe("formatSubscriptionQuota", () => {
  const base: Subscription = {
    id: "s",
    name: "sub",
    url: "https://example.com",
    refreshIntervalMinutes: 60,
    autoRefresh: false,
    enabled: true,
  };

  it("formats remaining and expire", () => {
    expect(formatSubscriptionQuota(base)).toBe("");
    expect(
      formatSubscriptionQuota({
        ...base,
        trafficUpload: 1 * 1024 ** 3,
        trafficDownload: 2 * 1024 ** 3,
        trafficTotal: 10 * 1024 ** 3,
        expireAt: "2026-12-01T00:00:00Z",
      }),
    ).toBe("осталось 7.0 GiB · до 2026-12-01");
    expect(
      formatSubscriptionQuota({
        ...base,
        trafficUpload: 100,
        trafficDownload: 200,
      }),
    ).toBe("использовано 300 B");
  });
});

describe("decodeHeaderText / formatAnnounce", () => {
  it("decodes base64: prefix", () => {
    expect(decodeHeaderText("plain")).toBe("plain");
    expect(decodeHeaderText("base64:SGVsbG8=")).toBe("Hello");
    expect(formatAnnounce("base64:0J/QvtC00L/QuNGB0LrQsDogNDMxOTk3NzY2")).toBe(
      "Подписка: 431997766",
    );
  });
});

describe("formatSubscriptionDates", () => {
  const base: Subscription = {
    id: "s",
    name: "sub",
    url: "https://example.com",
    refreshIntervalMinutes: 60,
    autoRefresh: false,
    enabled: true,
  };

  it("formats created and updated", () => {
    expect(formatSubscriptionDates(base)).toBe("");
    expect(
      formatSubscriptionDates({
        ...base,
        createdAt: "2026-08-01T10:00:00+07:00",
        lastRefresh: "2026-08-13T22:00:00+07:00",
      }),
    ).toMatch(/^добавлена .+ · обновлена .+$/);
    expect(
      formatSubscriptionDates({
        ...base,
        lastRefresh: "2026-08-07T15:21:03+07:00",
      }),
    ).toMatch(/^обновлена /);
    expect(
      formatSubscriptionDates({
        ...base,
        createdAt: "0001-01-01T00:00:00Z",
        lastRefresh: "2026-08-07T15:21:03+07:00",
      }),
    ).toMatch(/^обновлена /);
    expect(
      formatSubscriptionDates({
        ...base,
        createdAt: "2026-08-07T15:21:03+07:00",
        lastRefresh: "2026-08-07T15:21:03+07:00",
      }),
    ).toMatch(/^добавлена /);
  });
});

describe("formatSubscriptionStatusLine", () => {
  const base: Subscription = {
    id: "s",
    name: "sub",
    url: "https://example.com",
    refreshIntervalMinutes: 60,
    autoRefresh: true,
    enabled: true,
    selectedNodeId: "n1",
    nodeCount: 306,
  };

  it("joins refresh, selected, count", () => {
    expect(formatServerCount(1)).toBe("1 сервер");
    expect(formatServerCount(2)).toBe("2 сервера");
    expect(formatServerCount(5)).toBe("5 серверов");
    expect(formatSubscriptionStatusLine(base, true)).toBe(
      "авто каждые 1 ч. · сервер выбран · 306 серверов",
    );
    expect(
      formatSubscriptionStatusLine({ ...base, url: "", autoRefresh: false, nodeCount: 1 }, false),
    ).toBe("локальный импорт · сервер выбран · 1 сервер");
  });
});

describe("formatSubscriptionType", () => {
  it("maps known sources", () => {
    expect(formatSubscriptionType("url")).toBe("URL-подписка");
    expect(formatSubscriptionType("ovpn")).toBe("OpenVPN (.ovpn)");
    expect(formatSubscriptionType("clash")).toBe("Clash / Mihomo");
    expect(formatSubscriptionType("uri")).toBe("Share-ссылка");
    expect(formatSubscriptionType("text")).toBe("Текст / список узлов");
    expect(formatSubscriptionType("")).toBe("Неизвестно");
  });
});
