import { describe, expect, it, vi } from "vitest";
import type { NetworkApp } from "./api";
import {
  ConnectionsController,
  filterNetworkApps,
  networkAppName,
  socketState,
  type ConnectionsState,
} from "./network-connections";

const apps: NetworkApp[] = [
  {
    pid: 42042,
    processName: "browser",
    execPath: "/opt/browser/app",
    connections: 2,
    sockets: [
      {
        protocol: "tcp",
        state: "ESTAB",
        localAddress: "192.0.2.1:50000",
        remoteAddress: "203.0.113.4:443",
      },
      { protocol: "udp", state: "UNCONN", localAddress: "[::1]:5353", remoteAddress: "[::]:*" },
    ],
  },
  {
    pid: 0,
    processName: "",
    connections: 1,
    sockets: [
      {
        protocol: "tcp",
        state: "TIME-WAIT",
        localAddress: "127.0.0.1:10000",
        remoteAddress: "127.0.0.1:8000",
      },
    ],
  },
];
const all = { query: "", protocol: "all" as const };

describe("network snapshot filters", () => {
  it.each(["BROWSER", "/opt/browser", "42042"])(
    "finds processes by name, path and PID: %s",
    (query) => {
      const found = filterNetworkApps(apps, { ...all, query });
      expect(found.map((a) => a.pid)).toEqual([42042]);
      expect(found[0].sockets).toHaveLength(2);
    },
  );
  it("combines protocol with address/port, preserving the original snapshot", () => {
    const before = JSON.stringify(apps);
    const found = filterNetworkApps(apps, { query: "  5353 ", protocol: "udp" });
    expect(found[0].sockets).toHaveLength(1);
    expect(found[0].connections).toBe(2);
    expect(filterNetworkApps(apps, { query: "5353", protocol: "tcp" })).toEqual([]);
    expect(JSON.stringify(apps)).toBe(before);
  });
  it("finds state labels and preserves ownerless sockets", () => {
    expect(filterNetworkApps(apps, { ...all, query: "ожидание освобождения" })[0].pid).toBe(0);
    expect(networkAppName(apps[1])).toBe("Процесс не определён");
    expect(networkAppName({ ...apps[1], pid: 99 })).toBe("Процесс #99");
    expect(socketState("NEW-STATE")).toBe("NEW-STATE");
  });
  it("handles old API snapshots without inventing details", () => {
    const old = { pid: 1, processName: "old", connections: 3 };
    expect(filterNetworkApps([old], all)).toEqual([old]);
    expect(filterNetworkApps([old], { ...all, protocol: "udp" })).toEqual([]);
    expect(filterNetworkApps([], all)).toEqual([]);
  });
  it("does not hide processes beyond the old 40-process limit", () => {
    const many = Array.from({ length: 80 }, (_, i) => ({ ...apps[0], pid: i + 1 }));
    expect(filterNetworkApps(many, all)).toHaveLength(80);
  });
});

describe("snapshot refresh", () => {
  it("preserves previous data and timestamp when a scan fails", async () => {
    const scan = vi.fn().mockResolvedValueOnce(apps).mockRejectedValueOnce(new Error("ss failed"));
    let state: ConnectionsState | null = null;
    const controller = new ConnectionsController(scan, (next) => {
      state = next;
    });
    await controller.refresh();
    const before = state! as ConnectionsState;
    await controller.refresh();
    const failed = state! as ConnectionsState;
    expect(failed.apps).toBe(before.apps);
    expect(failed.receivedAt).toBe(before.receivedAt);
    expect(failed.pending).toBe(false);
    expect(failed.error).toBe("ss failed");
  });
  it("suppresses overlapping scans and clears the previous error after success", async () => {
    let finish!: (apps: NetworkApp[]) => void;
    const scan = vi
      .fn()
      .mockRejectedValueOnce(new Error("offline"))
      .mockImplementationOnce(
        () =>
          new Promise<NetworkApp[]>((resolve) => {
            finish = resolve;
          }),
      );
    const states: ConnectionsState[] = [];
    const controller = new ConnectionsController(scan, (next) => states.push(next));
    await controller.refresh();
    const refreshing = controller.refresh();
    await controller.refresh();
    expect(scan).toHaveBeenCalledTimes(2);
    expect(states[states.length - 1]?.pending).toBe(true);
    finish(apps);
    await refreshing;
    expect(states[states.length - 1]?.error).toBe("");
    expect(states[states.length - 1]?.apps).toBe(apps);
  });
  it("accepts the old null empty response without leaving the button pending", async () => {
    const changed = vi.fn();
    const controller = new ConnectionsController(async () => null, changed);
    await controller.refresh();
    expect(changed.mock.lastCall?.[0]).toMatchObject({ apps: [], pending: false, error: "" });
  });
});
