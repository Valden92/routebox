import { describe, expect, it, vi } from "vitest";
import type { api } from "./api";
import { saveRule } from "./rule-mutations";

describe("deny API compatibility", () => {
  it.each([undefined, 7])("does not send a deny to an unsupported daemon (%s)", async (version) => {
    const request = vi.fn().mockResolvedValue({ apiVersion: version });
    await expect(
      saveRule("/api/rules/one", "PUT", { path: "block" }, request as typeof api),
    ).rejects.toThrow("новый демон");
    expect(request.mock.calls).toEqual([["/api/version"]]);
  });
  it("does not send the mutation when version lookup fails", async () => {
    const request = vi.fn().mockRejectedValue(new Error("offline"));
    await expect(
      saveRule("/api/rules", "POST", { pattern: "ads.test", path: "block" }, request as typeof api),
    ).rejects.toThrow("offline");
    expect(request).toHaveBeenCalledTimes(1);
  });
  it.each(["POST", "PUT"] as const)(
    "sends %s deny only after verifying support",
    async (method) => {
      const request = vi
        .fn()
        .mockResolvedValueOnce({ apiVersion: 8 })
        .mockResolvedValueOnce({ status: "updated" });
      const body = { pattern: "*.ads.test", path: "block" as const };
      await saveRule("/api/rules", method, body, request as typeof api);
      expect(request.mock.calls).toEqual([
        ["/api/version"],
        ["/api/rules", { method, body: JSON.stringify(body) }],
      ]);
    },
  );
  it("does not introduce a version dependency for disabling or routing rules", async () => {
    const request = vi.fn().mockResolvedValue({});
    await saveRule("/api/rules/one", "PUT", { enabled: false }, request as typeof api);
    await saveRule("/api/rules/one", "PUT", { path: "direct" }, request as typeof api);
    expect(request.mock.calls.every(([path]) => path === "/api/rules/one")).toBe(true);
  });
});
