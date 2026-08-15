import { describe, expect, it } from "vitest";
import { classifyQrPayload, previewQrValue } from "./qr-import";

describe("classifyQrPayload", () => {
  it("maps https subscription URL", () => {
    expect(classifyQrPayload("  https://ex.com/sub?token=1  ")).toEqual({
      target: "url",
      value: "https://ex.com/sub?token=1",
    });
  });

  it("maps http URL", () => {
    expect(classifyQrPayload("http://ex.com/a")).toEqual({
      target: "url",
      value: "http://ex.com/a",
    });
  });

  it("maps single share URI to uri tab", () => {
    expect(classifyQrPayload("vless://uuid@host:443?type=tcp#n")).toEqual({
      target: "uri",
      value: "vless://uuid@host:443?type=tcp#n",
    });
    expect(classifyQrPayload("hy2://x@h:443")).toEqual({
      target: "uri",
      value: "hy2://x@h:443",
    });
    expect(classifyQrPayload("ss://YWJj@h:1")).toEqual({
      target: "uri",
      value: "ss://YWJj@h:1",
    });
  });

  it("maps multi-line or yaml to text", () => {
    expect(classifyQrPayload("vless://a\nvless://b")).toEqual({
      target: "text",
      value: "vless://a\nvless://b",
    });
    const yaml = "proxies:\n  - name: x\n    type: ss\n    server: a\n    port: 1";
    expect(classifyQrPayload(yaml)?.target).toBe("text");
  });

  it("returns null for empty", () => {
    expect(classifyQrPayload("")).toBeNull();
    expect(classifyQrPayload("   ")).toBeNull();
  });
});

describe("previewQrValue", () => {
  it("truncates long strings", () => {
    expect(previewQrValue("abc", 10)).toBe("abc");
    expect(previewQrValue("abcdefghijklmnopqrstuvwxyz", 10).endsWith("…")).toBe(true);
  });
});
