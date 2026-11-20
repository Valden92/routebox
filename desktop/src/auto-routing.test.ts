import { describe, expect, it, vi } from "vitest";
import type { api, AutoRoutingSettings } from "./api";
import {
  AutoRoutingController,
  autoRoutingStatus,
  autoRoutingTransport,
  type AutoRoutingView,
} from "./auto-routing";

function fixture() {
  const transport = {
    load: vi
      .fn<() => Promise<AutoRoutingSettings>>()
      .mockResolvedValue({ autoDetectRouting: true }),
    save: vi.fn<(enabled: boolean) => Promise<AutoRoutingSettings>>().mockResolvedValue({
      autoDetectRouting: false,
    }),
  };
  const changed = vi.fn<(view: AutoRoutingView) => void>();
  const controller = new AutoRoutingController(transport, changed);
  const view = () => changed.mock.lastCall![0];
  return { transport, controller, view };
}

describe("background routing setting", () => {
  it("keeps the control unavailable until the setting is loaded", async () => {
    const f = fixture();
    expect(await f.controller.setEnabled(false)).toBe(false);
    expect(f.transport.save).not.toHaveBeenCalled();
    await f.controller.refresh();
    expect(f.view()).toEqual({ enabled: true, pending: false, error: "" });
  });

  it("shows the new value immediately and rejects a second action while saving", async () => {
    const f = fixture();
    await f.controller.refresh();
    let resolve!: (value: AutoRoutingSettings) => void;
    f.transport.save.mockReturnValueOnce(new Promise((yes) => (resolve = yes)));
    const saving = f.controller.setEnabled(false);
    expect(f.view()).toEqual({ enabled: false, pending: true, error: "" });
    expect(await f.controller.setEnabled(true)).toBe(false);
    await f.controller.refresh();
    expect(f.transport.load).toHaveBeenCalledTimes(1);
    expect(f.transport.save).toHaveBeenCalledTimes(1);
    resolve({ autoDetectRouting: false });
    expect(await saving).toBe(true);
    expect(f.view().pending).toBe(false);
    expect(autoRoutingStatus(f.view())).toContain("выключено");
    expect(autoRoutingStatus(f.view())).toContain("ручная проверка");
  });

  it("restores the acknowledged state after failure and allows retry", async () => {
    const f = fixture();
    await f.controller.refresh();
    f.transport.save.mockRejectedValueOnce(new Error("offline"));
    await expect(f.controller.setEnabled(false)).rejects.toThrow("offline");
    expect(f.view()).toEqual({ enabled: true, pending: false, error: "" });
    expect(await f.controller.setEnabled(false)).toBe(true);
  });

  it("supports a failed load followed by retry without enabling writes", async () => {
    const f = fixture();
    f.transport.load.mockRejectedValueOnce(new Error("offline"));
    await f.controller.refresh();
    expect(f.view().enabled).toBe(null);
    expect(autoRoutingStatus(f.view())).toContain("Не удалось");
    expect(await f.controller.setEnabled(true)).toBe(false);
    await f.controller.refresh();
    expect(f.view().enabled).toBe(true);
  });

  it("does not enable the control on a daemon with destructive partial PUT semantics", async () => {
    const request = vi.fn().mockResolvedValue({ apiVersion: 6 });
    const changed = vi.fn();
    const controller = new AutoRoutingController(
      autoRoutingTransport(request as typeof api),
      changed,
    );
    await controller.refresh();
    expect(await controller.setEnabled(false)).toBe(false);
    expect(request.mock.calls).toEqual([["/api/version"]]);
  });

  it("sends only the learning flag, preserving unrelated server settings", async () => {
    const request = vi
      .fn()
      .mockResolvedValueOnce({ apiVersion: 7 })
      .mockResolvedValueOnce({ autoDetectRouting: true })
      .mockResolvedValueOnce({ autoDetectRouting: false });
    const controller = new AutoRoutingController(
      autoRoutingTransport(request as typeof api),
      vi.fn(),
    );
    await controller.refresh();
    await controller.setEnabled(false);
    expect(request.mock.lastCall).toEqual([
      "/api/settings",
      { method: "PUT", body: JSON.stringify({ autoDetectRouting: false }) },
    ]);
  });
});
