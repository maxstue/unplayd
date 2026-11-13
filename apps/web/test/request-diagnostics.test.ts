import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { upstream } from "../server/clients/request";

beforeEach(() => {
  vi.spyOn(console, "warn").mockImplementation(() => {});
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe("safe upstream diagnostics", () => {
  it.each([
    { cause: new Error("https://private-service/?token=secret"), category: "transport" },
    { cause: new DOMException("private timeout token", "TimeoutError"), category: "timeout" },
    { cause: new SyntaxError("private response body"), category: "parsing" },
  ])(
    "classifies $category failures without logging private provider details",
    async ({ cause, category }) => {
      await expect(
        upstream(
          async () => {
            throw cause;
          },
          "user",
          "request_safe",
        ),
      ).rejects.toMatchObject({ statusCode: 503 });
      expect(console.warn).toHaveBeenCalledExactlyOnceWith("upstream_failure", {
        requestId: "request_safe",
        service: "user",
        category,
        status: null,
      });
      expect(JSON.stringify(vi.mocked(console.warn).mock.calls)).not.toContain(cause.message);
    },
  );

  it.each([
    { service: "user" as const, status: 401, expected: 401 },
    { service: "user" as const, status: 403, expected: 403 },
    { service: "catalog" as const, status: 401, expected: 503 },
    { service: "catalog" as const, status: 500, expected: 503 },
  ])(
    "records the $service HTTP status $status while preserving its public mapping",
    async ({ service, status, expected }) => {
      await expect(
        upstream(
          async () => ({ response: Response.json({ token: "private body" }, { status }) }),
          service,
          "request_http",
        ),
      ).rejects.toMatchObject({ statusCode: expected });
      expect(console.warn).toHaveBeenCalledExactlyOnceWith("upstream_failure", {
        requestId: "request_http",
        service,
        category: "http",
        status,
      });
      expect(JSON.stringify(vi.mocked(console.warn).mock.calls)).not.toContain("private body");
    },
  );

  it("reports a missing success payload with its status but permits explicitly empty responses", async () => {
    const empty = async () => ({ response: new Response(null, { status: 204 }) });
    await expect(upstream(empty, "user", "request_empty")).rejects.toMatchObject({
      statusCode: 503,
    });
    expect(console.warn).toHaveBeenCalledExactlyOnceWith("upstream_failure", {
      requestId: "request_empty",
      service: "user",
      category: "parsing",
      status: 204,
    });
    vi.mocked(console.warn).mockClear();
    await expect(
      upstream(empty, "user", "request_empty", { allowEmpty: true }),
    ).resolves.toMatchObject({ status: 204 });
    expect(console.warn).not.toHaveBeenCalled();
  });

  it("suppresses client abort diagnostics even if fetch rejects with a plain transport error", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(
      upstream(
        async () => {
          throw new Error("private cancellation");
        },
        "catalog",
        "request_abort",
        { signal: controller.signal },
      ),
    ).rejects.toMatchObject({ statusCode: 503 });
    expect(console.warn).not.toHaveBeenCalled();
  });

  it("does not copy an invalid correlation value into structured logs", async () => {
    await expect(
      upstream(
        async () => {
          throw new Error("private transport");
        },
        "user",
        "https://private/?token=secret",
      ),
    ).rejects.toMatchObject({ statusCode: 503 });
    expect(console.warn).toHaveBeenCalledExactlyOnceWith("upstream_failure", {
      requestId: "unknown",
      service: "user",
      category: "transport",
      status: null,
    });
  });
});
