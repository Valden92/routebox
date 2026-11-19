import { describe, expect, it, vi } from "vitest";
import type { AutoSelectStartResponse } from "./api";
import { AutoSelectSession } from "./autoselect-session";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

const job: AutoSelectStartResponse = {
  jobId: "old",
  subscriptionId: "sub",
  total: 312,
  phase: "pinging",
};

describe("AutoSelectSession", () => {
  it("does not start a job after the modal was closed", async () => {
    const cancel = vi.fn(async () => {});
    const session = new AutoSelectSession(null, cancel);
    await session.close();
    const start = vi.fn(async () => job);
    expect(await session.start(start)).toBeNull();
    expect(start).not.toHaveBeenCalled();
    expect(cancel).not.toHaveBeenCalled();
    expect(session.signal.aborted).toBe(true);
  });

  it("cancels a running job exactly once and aborts polling", async () => {
    const cancel = vi.fn(async () => {});
    const session = new AutoSelectSession(null, cancel);
    expect(await session.start(async () => job)).toEqual(job);
    const stop = session.close();
    expect(session.close()).toBe(stop);
    await stop;
    expect(cancel).toHaveBeenCalledExactlyOnceWith(job);
    expect(session.signal.aborted).toBe(true);
  });

  it("waits for a late jobId and cancels it without rendering a stale result", async () => {
    const sent = deferred<void>();
    const response = deferred<AutoSelectStartResponse>();
    const cancel = vi.fn(async () => {});
    const session = new AutoSelectSession(null, cancel);
    const start = session.start(() => {
      sent.resolve();
      return response.promise;
    });
    await sent.promise;
    const stop = session.close();
    expect(cancel).not.toHaveBeenCalled();
    response.resolve(job);
    await stop;
    expect(await start).toBeNull();
    expect(cancel).toHaveBeenCalledExactlyOnceWith(job);
  });

  it("starts the next job only after the old probe has finished cleanup", async () => {
    const stopping = deferred<void>();
    const stopped = deferred<void>();
    const old = new AutoSelectSession(null, async () => {
      stopping.resolve();
      await stopped.promise;
    });
    await old.start(async () => job);
    const next = new AutoSelectSession(old, async () => {});
    const start = vi.fn(async () => ({ ...job, jobId: "new" }));
    const result = next.start(start);
    await stopping.promise;
    expect(start).not.toHaveBeenCalled();
    expect(old.closed).toBe(true);
    stopped.resolve();
    expect((await result)?.jobId).toBe("new");
    expect(start).toHaveBeenCalledTimes(1);
  });

  it("keeps waiting for cleanup across rapid open-close-open sequences", async () => {
    const stopping = deferred<void>();
    const stopped = deferred<void>();
    const cancel = vi.fn(async () => {
      stopping.resolve();
      await stopped.promise;
    });
    const first = new AutoSelectSession(null, cancel);
    await first.start(async () => job);
    const second = new AutoSelectSession(first, async () => {});
    const secondRequest = vi.fn(async () => ({ ...job, jobId: "second" }));
    const secondStart = second.start(secondRequest);
    await stopping.promise;
    const secondStop = second.close();
    const third = new AutoSelectSession(second, async () => {});
    const thirdRequest = vi.fn(async () => ({ ...job, jobId: "third" }));
    const thirdStart = third.start(thirdRequest);
    expect(thirdRequest).not.toHaveBeenCalled();
    stopped.resolve();
    await secondStop;
    expect(await secondStart).toBeNull();
    expect(secondRequest).not.toHaveBeenCalled();
    expect((await thirdStart)?.jobId).toBe("third");
    expect(cancel).toHaveBeenCalledTimes(1);
  });

  it("does not invent a jobId after a failed start", async () => {
    const cancel = vi.fn(async () => {});
    const session = new AutoSelectSession(null, cancel);
    await expect(
      session.start(async () => Promise.reject(new Error("start failed"))),
    ).rejects.toThrow("start failed");
    await session.close();
    expect(cancel).not.toHaveBeenCalled();
  });

  it("allows cancellation to be retried after a temporary API failure", async () => {
    const cancel = vi.fn<() => Promise<void>>();
    cancel.mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce(undefined);
    const old = new AutoSelectSession(null, cancel);
    await old.start(async () => job);
    await expect(old.close()).rejects.toThrow("offline");
    const next = new AutoSelectSession(old, async () => {});
    expect((await next.start(async () => ({ ...job, jobId: "new" })))?.jobId).toBe("new");
    expect(cancel).toHaveBeenCalledTimes(2);
  });
});
