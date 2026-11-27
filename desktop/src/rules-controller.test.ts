import { describe, expect, it, vi } from "vitest";
import { ApiError, type DomainRule, type RulesResponse } from "./api";
import {
  RulesController,
  ruleAdditionMessage,
  ruleMutationError,
  setRuleButtonPending,
  type RulesView,
} from "./rules-controller";

const first: DomainRule = { id: "1", pattern: "one.invalid", path: "personal", enabled: true };
const second: DomainRule = { id: "2", pattern: "two.invalid", path: "direct", enabled: true };
const rule = { pattern: "new.invalid", path: "personal" as const };
const data = (domains: DomainRule[] = [first, second]): RulesResponse => ({ domains, apps: [] });

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

function fixture() {
  const transport = {
    load: vi.fn<() => Promise<RulesResponse>>().mockResolvedValue(data()),
    add: vi.fn<() => Promise<unknown>>().mockResolvedValue({}),
    remove: vi.fn<() => Promise<unknown>>().mockResolvedValue({}),
    clear: vi.fn<() => Promise<{ deleted: number }>>().mockResolvedValue({ deleted: 2 }),
  };
  const changed = vi.fn<(view: RulesView) => void>();
  const controller = new RulesController(transport, changed);
  const view = () => changed.mock.calls[changed.mock.calls.length - 1][0];
  return { controller, transport, changed, view };
}

describe("responsive rules actions", () => {
  it.each([
    ["created", "Правило добавлено"],
    ["updated", "Правило обновлено"],
    ["unchanged", "Правило уже существует"],
  ] as const)("reports the actual add outcome: %s", async (status, message) => {
    const f = fixture();
    f.transport.add.mockResolvedValue({ status });
    const result = await f.controller.add(rule);
    expect(result).toBe(status);
    expect(ruleAdditionMessage(status)).toBe(message);
    expect(f.view().adding).toBe(false);
  });
  it("accepts null lists from a daemon with no configured rules", async () => {
    const f = fixture();
    f.transport.load.mockResolvedValue({ domains: null, apps: null } as unknown as RulesResponse);
    await f.controller.refresh();
    expect(f.view().data).toEqual({ domains: [], apps: [] });
    expect(await f.controller.add(rule)).toBe("created");
  });
  it("immediately locks add, rejects double submission, and stays busy through list refresh", async () => {
    const f = fixture();
    await f.controller.refresh();
    const request = deferred<unknown>();
    const refresh = deferred<RulesResponse>();
    f.transport.add.mockReturnValueOnce(request.promise);
    f.transport.load.mockReturnValueOnce(refresh.promise);
    const adding = f.controller.add(rule);
    expect(f.view().adding).toBe(true);
    expect(await f.controller.add(rule)).toBe(false);
    expect(f.transport.add).toHaveBeenCalledTimes(1);
    request.resolve({});
    await Promise.resolve();
    expect(f.view().adding).toBe(true);
    refresh.resolve(data([...data().domains, { ...first, id: "3", pattern: rule.pattern }]));
    expect(await adding).toBe("created");
    expect(f.view().adding).toBe(false);
    expect(f.view().data!.domains).toHaveLength(3);
  });

  it("releases add after failure and preserves the actual server list", async () => {
    const f = fixture();
    await f.controller.refresh();
    f.transport.add.mockRejectedValueOnce(new Error("offline"));
    await expect(f.controller.add(rule)).rejects.toThrow("offline");
    expect(f.view().adding).toBe(false);
    expect(await f.controller.add(rule)).toBe("created");
  });

  it("hides delete immediately, updates counts, and ignores duplicate clicks", async () => {
    const f = fixture();
    await f.controller.refresh();
    const request = deferred<unknown>();
    f.transport.remove.mockReturnValueOnce(request.promise);
    const deleting = f.controller.remove(first.id);
    expect(f.view().data!.domains).toEqual([second]);
    expect(f.view().deleting).toBe(1);
    expect(await f.controller.remove(first.id)).toBe(false);
    expect(f.transport.remove).toHaveBeenCalledTimes(1);
    f.transport.load.mockResolvedValue(data([second]));
    request.resolve({});
    expect(await deleting).toBe(true);
    expect(f.view().deleting).toBe(0);
    expect(f.view().data!.domains).toEqual([second]);
  });

  it("does not resurrect a pending delete when polling returns an old list", async () => {
    const f = fixture();
    await f.controller.refresh();
    const request = deferred<unknown>();
    f.transport.remove.mockReturnValueOnce(request.promise);
    const deleting = f.controller.remove(first.id);
    await f.controller.refresh();
    expect(f.view().data!.domains).toEqual([second]);
    f.transport.load.mockResolvedValue(data([second]));
    request.resolve({});
    await deleting;
    expect(f.view().data!.domains).toEqual([second]);
  });

  it("restores a failed deletion in its original position", async () => {
    const f = fixture();
    await f.controller.refresh();
    f.transport.remove.mockRejectedValueOnce(new Error("write failed"));
    await expect(f.controller.remove(first.id)).rejects.toThrow("write failed");
    expect(f.view().data!.domains).toEqual([first, second]);
    expect(f.view().deleting).toBe(0);
  });

  it("restores the cached row when both deletion and reconciliation fail", async () => {
    const f = fixture();
    await f.controller.refresh();
    f.transport.remove.mockRejectedValueOnce(new Error("offline"));
    f.transport.load.mockRejectedValueOnce(new Error("offline"));
    await expect(f.controller.remove(first.id)).rejects.toThrow("offline");
    expect(f.view().data!.domains).toEqual([first, second]);
  });

  it("does not restore a persisted deletion when reapply fails, even if GET is offline", async () => {
    const f = fixture();
    await f.controller.refresh();
    const error = new ApiError(500, "reapply_failed", "cannot apply");
    f.transport.remove.mockRejectedValueOnce(error);
    f.transport.load.mockRejectedValueOnce(new Error("offline"));
    await expect(f.controller.remove(first.id)).rejects.toBe(error);
    expect(f.view().data!.domains).toEqual([second]);
  });

  it("reconciles a persisted addition even when reapply reports failure", async () => {
    const f = fixture();
    await f.controller.refresh();
    f.transport.add.mockRejectedValueOnce(new ApiError(500, "reapply_failed", "cannot apply"));
    f.transport.load.mockResolvedValue(data([first, second, { ...first, id: "3" }]));
    await expect(f.controller.add(rule)).rejects.toThrow("cannot apply");
    expect(f.view().data!.domains).toHaveLength(3);
    expect(f.view().adding).toBe(false);
  });

  it("ignores a GET started before a mutation and queues a fresh read after it", async () => {
    const f = fixture();
    await f.controller.refresh();
    const old = deferred<RulesResponse>();
    const deletion = deferred<unknown>();
    f.transport.load.mockReturnValueOnce(old.promise).mockResolvedValueOnce(data([second]));
    f.transport.remove.mockReturnValueOnce(deletion.promise);
    const poll = f.controller.refresh();
    const removing = f.controller.remove(first.id);
    deletion.resolve({});
    await Promise.resolve();
    old.resolve(data());
    await Promise.all([poll, removing]);
    expect(f.view().data!.domains).toEqual([second]);
    expect(f.transport.load).toHaveBeenCalledTimes(3);
  });

  it("keeps an acknowledged deletion hidden if its stale poll finishes and the queued GET fails", async () => {
    const f = fixture();
    await f.controller.refresh();
    const old = deferred<RulesResponse>();
    const deletion = deferred<unknown>();
    f.transport.remove.mockReturnValueOnce(deletion.promise);
    const removing = f.controller.remove(first.id);
    f.transport.load.mockReturnValueOnce(old.promise).mockRejectedValueOnce(new Error("offline"));
    const poll = f.controller.refresh();
    deletion.resolve({});
    await Promise.resolve();
    old.resolve(data());
    await Promise.all([poll.catch(() => {}), removing]);
    expect(f.view().data!.domains).toEqual([second]);
  });

  it("retries a failed old poll when a fresh forced read is queued", async () => {
    const f = fixture();
    const old = deferred<RulesResponse>();
    f.transport.load.mockReturnValueOnce(old.promise).mockResolvedValueOnce(data([second]));
    const poll = f.controller.refresh();
    const forced = f.controller.refresh(true);
    old.reject(new Error("old poll failed"));
    await Promise.all([poll, forced]);
    expect(f.view().data!.domains).toEqual([second]);
  });

  it("handles multiple simultaneous removals without losing unrelated rows", async () => {
    const f = fixture();
    await f.controller.refresh();
    const a = deferred<unknown>();
    const b = deferred<unknown>();
    f.transport.remove.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
    const firstDelete = f.controller.remove(first.id);
    const secondDelete = f.controller.remove(second.id);
    expect(f.view().data!.domains).toEqual([]);
    expect(f.view().deleting).toBe(2);
    f.transport.load.mockResolvedValue(data([]));
    a.resolve({});
    b.resolve({});
    await Promise.all([firstDelete, secondDelete]);
    expect(f.view().deleting).toBe(0);
    expect(f.view().data!.domains).toEqual([]);
  });
});

describe("pressed button feedback", () => {
  it("holds a button down and disabled until completion, then restores it", () => {
    const toggle = vi.fn();
    const setAttribute = vi.fn();
    const button = {
      disabled: false,
      classList: { toggle },
      setAttribute,
    } as unknown as HTMLButtonElement;
    setRuleButtonPending(button, true);
    expect(button.disabled).toBe(true);
    expect(toggle).toHaveBeenLastCalledWith("is-pending", true);
    expect(setAttribute).toHaveBeenLastCalledWith("aria-busy", "true");
    setRuleButtonPending(button, false);
    expect(button.disabled).toBe(false);
    expect(toggle).toHaveBeenLastCalledWith("is-pending", false);
    expect(setAttribute).toHaveBeenLastCalledWith("aria-busy", "false");
  });
  it("explains saved-but-not-applied changes without claiming rollback", () => {
    const error = new ApiError(500, "reapply_failed", "failure");
    expect(ruleMutationError(error, "add")).toContain("Правило сохранено");
    expect(ruleMutationError(error, "remove")).toContain("Правило удалено");
    expect(ruleMutationError(new Error("offline"), "add")).toBe("offline");
    expect(ruleMutationError(null, "remove")).toBe("Не удалось изменить правило");
  });
});

describe("bulk rule cleanup", () => {
  it("hides the snapshot immediately, blocks competing mutations and keeps new rules", async () => {
    const f = fixture();
    await f.controller.refresh();
    const request = deferred<{ deleted: number }>();
    f.transport.clear.mockReturnValueOnce(request.promise);
    const pending = f.controller.clear("manual", ["1", "2", "1"]);
    expect(f.view().data!.domains).toEqual([]);
    expect(f.view().clearing).toBe("manual");
    expect(f.transport.clear).toHaveBeenCalledWith("manual", ["1", "2"]);
    expect(await f.controller.clear("manual", ["1"])).toBe(false);
    expect(await f.controller.add(rule)).toBe(false);
    expect(await f.controller.remove("1")).toBe(false);
    await f.controller.refresh();
    expect(f.view().data!.domains).toEqual([]);
    const fresh = { ...first, id: "new" };
    f.transport.load.mockResolvedValue(data([fresh]));
    request.resolve({ deleted: 2 });
    expect(await pending).toBe(2);
    expect(f.view().data!.domains).toEqual([fresh]);
    expect(f.view().clearing).toBeNull();
    expect(f.view().deleting).toBe(0);
  });
  it("rejects empty cleanup and cleanup during an individual mutation", async () => {
    const f = fixture();
    await f.controller.refresh();
    expect(await f.controller.clear("auto", [])).toBe(false);
    const request = deferred<unknown>();
    f.transport.add.mockReturnValueOnce(request.promise);
    const adding = f.controller.add(rule);
    expect(await f.controller.clear("manual", ["1"])).toBe(false);
    request.resolve({});
    await adding;
    f.transport.remove.mockReturnValueOnce(request.promise);
    const removing = f.controller.remove("1");
    expect(await f.controller.clear("manual", ["2"])).toBe(false);
    await removing;
    expect(f.transport.clear).not.toHaveBeenCalled();
  });
  it("restores rows if saving fails and reconciliation is unavailable", async () => {
    const f = fixture();
    await f.controller.refresh();
    f.transport.clear.mockRejectedValueOnce(new Error("write failed"));
    f.transport.load.mockRejectedValueOnce(new Error("offline"));
    await expect(f.controller.clear("manual", ["1", "2"])).rejects.toThrow("write failed");
    expect(f.view().data!.domains).toEqual([first, second]);
    expect(f.view().clearing).toBeNull();
  });
  it("does not resurrect acknowledged deletion if GET fails", async () => {
    const f = fixture();
    await f.controller.refresh();
    f.transport.load.mockRejectedValueOnce(new Error("offline"));
    expect(await f.controller.clear("manual", ["1", "2"])).toBe(2);
    expect(f.view().data!.domains).toEqual([]);
  });
  it("does not resurrect saved deletion after publication failure", async () => {
    const f = fixture();
    await f.controller.refresh();
    f.transport.clear.mockRejectedValueOnce(new ApiError(500, "reapply_failed", "apply failed"));
    f.transport.load.mockRejectedValueOnce(new Error("offline"));
    await expect(f.controller.clear("manual", ["1", "2"])).rejects.toThrow("apply failed");
    expect(f.view().data!.domains).toEqual([]);
    expect(f.view().clearing).toBeNull();
  });
  it("reconciles a changed-source conflict instead of forgetting its rules", async () => {
    const f = fixture();
    await f.controller.refresh();
    const changed = { ...first, source: "auto" as const };
    f.transport.clear.mockRejectedValueOnce(new ApiError(409, "rules_changed", "changed"));
    f.transport.load.mockResolvedValue(data([changed, second]));
    await expect(f.controller.clear("manual", ["1", "2"])).rejects.toThrow("changed");
    expect(f.view().data!.domains).toEqual([changed, second]);
  });
  it("discards a pre-cleanup poll and queues a fresh read", async () => {
    const f = fixture();
    await f.controller.refresh();
    const old = deferred<RulesResponse>();
    const request = deferred<{ deleted: number }>();
    f.transport.load.mockReturnValueOnce(old.promise).mockResolvedValueOnce(data([]));
    f.transport.clear.mockReturnValueOnce(request.promise);
    const poll = f.controller.refresh();
    const clearing = f.controller.clear("manual", ["1", "2"]);
    request.resolve({ deleted: 2 });
    await Promise.resolve();
    old.resolve(data());
    await Promise.all([poll, clearing]);
    expect(f.view().data!.domains).toEqual([]);
    expect(f.transport.load).toHaveBeenCalledTimes(3);
  });
});
