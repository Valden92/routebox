import { afterEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, cancelAutoSelectJob } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("cancelAutoSelectJob", () => {
  const job = { jobId: "job/id", subscriptionId: "sub/id", phase: "probing", total: 312 };

  it("waits for successful cancellation using the job and subscription IDs", async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify({ finished: true })));
    vi.stubGlobal("fetch", fetch);
    await cancelAutoSelectJob(job);
    expect(fetch).toHaveBeenCalledWith(
      "http://127.0.0.1:47891/api/subscriptions/sub%2Fid/auto-select/job%2Fid/cancel",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("allows a new run after a completed job was pruned or the daemon restarted", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({ code: "auto_select_job_not_found", message: "job not found" }),
            {
              status: 404,
            },
          ),
      ),
    );
    await expect(cancelAutoSelectJob(job)).resolves.toBeUndefined();
  });

  it.each([404, 409, 500])("does not hide other API errors (%s)", async (status) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("request failed", { status })),
    );
    await expect(cancelAutoSelectJob(job)).rejects.toMatchObject({ status });
  });
});

describe("api", () => {
  it("parses JSON on success", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ status: "ok" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
      ),
    );
    await expect(api<{ status: string }>("/api/health")).resolves.toEqual({ status: "ok" });
  });

  it("throws ApiError with JSON body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ code: "reapply_failed", message: "boom" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
      ),
    );
    await expect(api("/api/x")).rejects.toMatchObject({
      status: 500,
      code: "reapply_failed",
      message: "boom",
    } satisfies Partial<ApiError>);
  });

  it("throws ApiError on plain text", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("not found", { status: 404 })),
    );
    try {
      await api("/api/x");
      expect.fail("should throw");
    } catch (e) {
      expect(e).toBeInstanceOf(ApiError);
      expect((e as ApiError).status).toBe(404);
      expect((e as ApiError).message).toBe("not found");
    }
  });
});
