import * as tauri from "@tauri-apps/api/core";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, cancelAutoSelectJob, createApiClient } from "./api";

vi.mock("@tauri-apps/api/core", () => ({
  isTauri: vi.fn(() => true),
  invoke: vi.fn(async () => "a".repeat(64)),
}));

const tokenA = "a".repeat(64);
const tokenB = "b".repeat(64);
function stubApiFetch(handler: typeof fetch) {
  vi.stubGlobal("fetch", handler);
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.mocked(tauri.isTauri).mockReturnValue(true);
  vi.mocked(tauri.invoke).mockReset().mockResolvedValue(tokenA);
});

describe("cancelAutoSelectJob", () => {
  const job = { jobId: "job/id", subscriptionId: "sub/id", phase: "probing", total: 312 };

  it("waits for successful cancellation using the job and subscription IDs", async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify({ finished: true })));
    stubApiFetch(fetch);
    await cancelAutoSelectJob(job);
    expect(fetch).toHaveBeenCalledWith(
      "http://127.0.0.1:47891/api/subscriptions/sub%2Fid/auto-select/job%2Fid/cancel",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("allows a new run after a completed job was pruned or the daemon restarted", async () => {
    stubApiFetch(
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
    stubApiFetch(vi.fn(async () => new Response("request failed", { status })));
    await expect(cancelAutoSelectJob(job)).rejects.toMatchObject({ status });
  });
});

describe("api", () => {
  it("parses JSON on success", async () => {
    stubApiFetch(
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
    stubApiFetch(
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
    stubApiFetch(vi.fn(async () => new Response("not found", { status: 404 })));
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

describe("authenticated API client", () => {
  it("does not bootstrap a credential over HTTP in an ordinary browser", async () => {
    vi.mocked(tauri.isTauri).mockReturnValue(false);
    const fetcher = vi.fn<typeof fetch>(async () => new Response("{}"));
    await expect(createApiClient(fetcher)("/api/settings")).rejects.toMatchObject({
      code: "api_desktop_required",
    });
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("keeps health/version public and adds the credential only to protected requests", async () => {
    const provider = vi.fn(async () => tokenA);
    const fetcher = vi.fn<typeof fetch>(async () => new Response("{}"));
    const request = createApiClient(fetcher, provider);
    await request("/api/health");
    await request("/api/version");
    expect(provider).not.toHaveBeenCalled();
    await request("/api/settings", {
      headers: { Accept: "application/json" },
      credentials: "include",
    });
    expect(provider).toHaveBeenCalledTimes(1);
    const init = fetcher.mock.calls[2][1]!;
    expect(new Headers(init.headers).get("Authorization")).toBe(`Bearer ${tokenA}`);
    expect(new Headers(init.headers).get("Accept")).toBe("application/json");
    expect(init.credentials).toBe("omit");
  });

  it("shares a pending credential read between concurrent callers", async () => {
    let release!: (token: string) => void;
    const provider = vi.fn(
      () =>
        new Promise<string>((resolve) => {
          release = resolve;
        }),
    );
    const fetcher = vi.fn<typeof fetch>(async () => new Response("{}"));
    const request = createApiClient(fetcher, provider);
    const a = request("/api/settings");
    const b = request("/api/rules");
    expect(provider).toHaveBeenCalledTimes(1);
    expect(fetcher).not.toHaveBeenCalled();
    release(tokenA);
    await Promise.all([a, b]);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("refreshes a rotated credential once after a daemon restart", async () => {
    const provider = vi.fn().mockResolvedValueOnce(tokenA).mockResolvedValueOnce(tokenB);
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ code: "api_auth_required" }), { status: 401 }),
      )
      .mockResolvedValueOnce(new Response("{}"));
    const request = createApiClient(fetcher, provider);
    await request("/api/rules", { method: "POST", body: "{}" });
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(provider).toHaveBeenCalledTimes(2);
    expect(new Headers(fetcher.mock.calls[1][1].headers).get("Authorization")).toBe(
      `Bearer ${tokenB}`,
    );
  });

  it.each([401, 403, 500])("does not replay a handler failure (%s)", async (status) => {
    const provider = vi.fn(async () => tokenA);
    const fetcher = vi.fn(async () => new Response("handler failed", { status }));
    await expect(
      createApiClient(fetcher, provider)("/api/unlock", { method: "POST" }),
    ).rejects.toMatchObject({ status });
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(provider).toHaveBeenCalledTimes(1);
  });

  it("does not replay a failed transport or loop on an invalid credential", async () => {
    const fetcher = vi.fn(async () => {
      throw new Error("offline");
    });
    await expect(
      createApiClient(fetcher, async () => tokenA)("/api/rules", { method: "DELETE" }),
    ).rejects.toThrow("offline");
    expect(fetcher).toHaveBeenCalledTimes(1);
    const alwaysUnauthorized = vi.fn(
      async () => new Response(JSON.stringify({ code: "api_auth_required" }), { status: 401 }),
    );
    await expect(
      createApiClient(alwaysUnauthorized, async () => tokenA)("/api/rules"),
    ).rejects.toMatchObject({ status: 401 });
    expect(alwaysUnauthorized).toHaveBeenCalledTimes(2);
  });

  it("does not let a late 401 discard an already refreshed credential", async () => {
    let first!: (r: Response) => void;
    let second!: (r: Response) => void;
    const provider = vi.fn().mockResolvedValueOnce(tokenA).mockResolvedValueOnce(tokenB);
    const fetcher = vi
      .fn()
      .mockImplementationOnce(
        () =>
          new Promise<Response>((resolve) => {
            first = resolve;
          }),
      )
      .mockImplementationOnce(
        () =>
          new Promise<Response>((resolve) => {
            second = resolve;
          }),
      )
      .mockImplementation(async () => new Response("{}"));
    const request = createApiClient(fetcher, provider);
    const a = request("/api/settings");
    const b = request("/api/rules");
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    first(new Response(JSON.stringify({ code: "api_auth_required" }), { status: 401 }));
    await a;
    second(new Response(JSON.stringify({ code: "api_auth_required" }), { status: 401 }));
    await b;
    expect(provider).toHaveBeenCalledTimes(2);
  });

  it("fails closed on malformed or unavailable private credentials", async () => {
    const fetcher = vi.fn<typeof fetch>(async () => new Response("{}"));
    for (const token of ["", "a\nb", "z".repeat(64)]) {
      await expect(
        createApiClient(fetcher, async () => token)("/api/settings"),
      ).rejects.toMatchObject({ code: "invalid_api_credential" });
    }
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("reads the native credential through Tauri IPC", async () => {
    vi.mocked(tauri.isTauri).mockReturnValue(true);
    const invoke = vi.mocked(tauri.invoke).mockResolvedValue(tokenA);
    const fetcher = vi.fn<typeof fetch>(async () => new Response("{}"));
    await createApiClient(fetcher)("/api/settings");
    expect(invoke).toHaveBeenCalledWith("local_api_token");
    invoke.mockRejectedValueOnce("private path details");
    await expect(createApiClient(fetcher)("/api/settings")).rejects.toMatchObject({
      code: "api_credential_unavailable",
    });
  });
});
