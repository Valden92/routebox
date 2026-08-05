import { describe, expect, it } from "vitest";
import { buildAddSubscriptionBody, isRemoteSubscriptionUrl } from "./add-subscription";

describe("buildAddSubscriptionBody", () => {
  it("builds url payload", () => {
    expect(
      buildAddSubscriptionBody({
        name: " My ",
        source: "url",
        url: " https://ex.com/sub ",
        autoRefresh: true,
        refreshIntervalMinutes: 30,
      }),
    ).toEqual({
      name: "My",
      source: "url",
      url: "https://ex.com/sub",
      autoRefresh: true,
      refreshIntervalMinutes: 30,
    });
  });

  it("maps file to text content", () => {
    expect(
      buildAddSubscriptionBody({
        name: "f",
        source: "file",
        content: "vless://x",
        autoRefresh: true,
        refreshIntervalMinutes: 60,
      }),
    ).toEqual({
      name: "f",
      source: "text",
      content: "vless://x",
      autoRefresh: false,
      refreshIntervalMinutes: 0,
    });
  });

  it("builds uri payload", () => {
    expect(
      buildAddSubscriptionBody({
        name: "n",
        source: "uri",
        content: "vless://a@b:1",
        autoRefresh: false,
        refreshIntervalMinutes: 0,
      }),
    ).toMatchObject({ source: "uri", content: "vless://a@b:1", autoRefresh: false });
  });
});

describe("isRemoteSubscriptionUrl", () => {
  it("detects http(s)", () => {
    expect(isRemoteSubscriptionUrl("https://x")).toBe(true);
    expect(isRemoteSubscriptionUrl("http://x")).toBe(true);
    expect(isRemoteSubscriptionUrl("")).toBe(false);
    expect(isRemoteSubscriptionUrl("vless://x")).toBe(false);
  });
});
