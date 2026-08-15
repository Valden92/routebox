import { describe, expect, it } from "vitest";
import {
  buildAddSubscriptionBody,
  isRemoteSubscriptionUrl,
  looksLikeClash,
  looksLikeOvpn,
  ovpnNeedsAuth,
} from "./add-subscription";

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

  it("maps .ovpn file to ovpn source with credentials", () => {
    const content = "client\nremote a 1194\nauth-user-pass\n<ca>\nX\n</ca>";
    expect(
      buildAddSubscriptionBody({
        name: "ov",
        source: "file",
        fileName: "work.ovpn",
        content: content + "\n",
        username: "user",
        password: "secret",
        autoRefresh: false,
        refreshIntervalMinutes: 0,
      }),
    ).toEqual({
      name: "ov",
      source: "ovpn",
      content,
      username: "user",
      password: "secret",
      autoRefresh: false,
      refreshIntervalMinutes: 0,
    });
  });

  it("maps .yaml file to clash source", () => {
    const content = "proxies:\n  - name: x\n    type: ss\n    server: a\n    port: 1";
    expect(
      buildAddSubscriptionBody({
        name: "c",
        source: "file",
        fileName: "sub.yaml",
        content: content + "\n",
        autoRefresh: true,
        refreshIntervalMinutes: 60,
      }),
    ).toEqual({
      name: "c",
      source: "clash",
      content,
      autoRefresh: false,
      refreshIntervalMinutes: 0,
    });
  });

  it("upgrades text clash yaml to clash source", () => {
    const content = "proxies:\n  - name: x\n    type: ss\n    server: a\n    port: 1";
    expect(
      buildAddSubscriptionBody({
        name: "c",
        source: "text",
        content: content + "\n",
        autoRefresh: false,
        refreshIntervalMinutes: 0,
      }),
    ).toEqual({
      name: "c",
      source: "clash",
      content,
      autoRefresh: false,
      refreshIntervalMinutes: 0,
    });
  });
});

describe("looksLikeOvpn / ovpnNeedsAuth / looksLikeClash", () => {
  it("detects by extension and content", () => {
    expect(looksLikeOvpn("", "x.ovpn")).toBe(true);
    expect(looksLikeOvpn("client\nproto udp\nremote h 1\n", "x.conf")).toBe(true);
    expect(looksLikeOvpn("vless://a", "x.txt")).toBe(false);
  });

  it("detects auth-user-pass", () => {
    expect(ovpnNeedsAuth("auth-user-pass\n")).toBe(true);
    expect(ovpnNeedsAuth("client\n")).toBe(false);
  });

  it("detects clash yaml", () => {
    expect(looksLikeClash("proxies:\n- type: ss\n  server: a\n", "x.yaml")).toBe(true);
    expect(looksLikeClash("vless://a", "x.txt")).toBe(false);
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
