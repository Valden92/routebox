import { beforeEach, describe, expect, it, vi } from "vitest";
import { isTauri } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { setupShutdownNotice, shutdownFailureMessage } from "./desktop-lifecycle";

vi.mock("@tauri-apps/api/core", () => ({ isTauri: vi.fn() }));
vi.mock("@tauri-apps/api/event", () => ({ listen: vi.fn() }));

describe("desktop shutdown feedback", () => {
  beforeEach(() => vi.clearAllMocks());
  it("does not access native events in a browser", async () => {
    vi.mocked(isTauri).mockReturnValue(false);
    await setupShutdownNotice(vi.fn());
    expect(listen).not.toHaveBeenCalled();
  });
  it("reports a failed close without displaying arbitrary native payloads", async () => {
    vi.mocked(isTauri).mockReturnValue(true);
    vi.mocked(listen).mockResolvedValue(() => {});
    const notify = vi.fn();
    await setupShutdownNotice(notify);
    expect(listen).toHaveBeenCalledWith("router-shutdown-failed", expect.any(Function));
    expect(notify).not.toHaveBeenCalled();
    const callback = vi.mocked(listen).mock.calls[0][1];
    callback({ event: "router-shutdown-failed", id: 1, payload: "private diagnostics" });
    expect(notify).toHaveBeenCalledWith(shutdownFailureMessage);
  });
});
