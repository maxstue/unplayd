import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { once } from "node:events";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { createApp, createError, defineEventHandler, toNodeListener } from "h3";
import { upstream } from "../server/clients/request";
import requestId from "../server/middleware/request-id";
import { logRequest } from "../server/request-log";

beforeEach(() => {
  vi.spyOn(console, "warn").mockImplementation(() => {});
  vi.spyOn(console, "log").mockImplementation(() => {});
});

/** Parses the single warning line and checks its envelope and duration. */
function warned() {
  expect(console.warn).toHaveBeenCalledOnce();
  const entry = JSON.parse(String(vi.mocked(console.warn).mock.calls[0]?.[0]));
  expect(entry).toMatchObject({ level: "WARN", msg: "upstream call" });
  expect(entry.durationMs).toEqual(expect.any(Number));
  expect(Date.parse(entry.time)).not.toBeNaN();
  const { requestId, service, outcome, status } = entry;
  return { requestId, service, outcome, status };
}
afterEach(() => {
  vi.restoreAllMocks();
});

describe("safe upstream diagnostics", () => {
  it.each([
    { cause: new Error("https://private-service/?token=secret"), outcome: "transport" },
    { cause: new DOMException("private timeout token", "TimeoutError"), outcome: "timeout" },
    { cause: new SyntaxError("private response body"), outcome: "parsing" },
  ])(
    "classifies $outcome failures without logging private provider details",
    async ({ cause, outcome }) => {
      await expect(
        upstream(
          async () => {
            throw cause;
          },
          "user",
          "request_safe",
        ),
      ).rejects.toMatchObject({ statusCode: 503 });
      expect(warned()).toEqual({
        requestId: "request_safe",
        service: "user",
        outcome,
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
      expect(warned()).toEqual({
        requestId: "request_http",
        service,
        outcome: "http",
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
    expect(warned()).toEqual({
      requestId: "request_empty",
      service: "user",
      outcome: "parsing",
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
    expect(warned()).toEqual({
      requestId: "unknown",
      service: "user",
      outcome: "transport",
      status: null,
    });
  });

  it("logs successful calls with status and duration at info level", async () => {
    await upstream(
      async () => ({ data: { ok: true }, response: new Response("{}") }),
      "catalog",
      "request_ok",
    );
    expect(console.warn).not.toHaveBeenCalled();
    expect(console.log).toHaveBeenCalledOnce();
    expect(JSON.parse(String(vi.mocked(console.log).mock.calls[0]?.[0]))).toMatchObject({
      level: "INFO",
      msg: "upstream call",
      requestId: "request_ok",
      service: "catalog",
      outcome: "ok",
      status: 200,
      durationMs: expect.any(Number),
    });
  });
});

describe("request log", () => {
  async function serve(path: string, headers: Record<string, string> = {}) {
    const app = createApp({ onRequest: logRequest })
      .use(requestId)
      .use(
        "/api/health",
        defineEventHandler(() => ({ status: "ok" })),
      )
      .use(
        "/api/games",
        defineEventHandler(() => {
          throw createError({ statusCode: 503 });
        }),
      );
    const server = createServer(toNodeListener(app)).listen(0);
    await once(server, "listening");
    const { port } = server.address() as AddressInfo;
    try {
      const response = await fetch(`http://127.0.0.1:${port}${path}`, { headers });
      await response.arrayBuffer();
      return response;
    } finally {
      server.close();
    }
  }

  it("writes one entry per request, also for errors, without query or headers", async () => {
    await serve("/api/games?q=private+search", {
      "X-Request-ID": "request_log",
      Authorization: "Bearer private-token",
    });
    await vi.waitFor(() => expect(console.log).toHaveBeenCalledOnce());
    const line = String(vi.mocked(console.log).mock.calls[0]?.[0]);
    expect(JSON.parse(line)).toMatchObject({
      level: "INFO",
      msg: "request completed",
      requestId: "request_log",
      method: "GET",
      path: "/api/games",
      status: 503,
      durationMs: expect.any(Number),
    });
    expect(line).not.toContain("private");
  });

  it("skips liveness probes", async () => {
    const response = await serve("/api/health");
    expect(response.status).toBe(200);
    expect(console.log).not.toHaveBeenCalled();
  });
});
