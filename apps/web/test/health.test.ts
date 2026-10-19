import { createApp, toWebHandler } from "h3";
import { expect, it } from "vite-plus/test";
import health from "../server/api/health.get";
import requestId from "../server/middleware/request-id";

it("returns the web service identity through the H3 HTTP boundary", async () => {
  const app = createApp().use(requestId).use("/api/health", health);
  const response = await toWebHandler(app)(new Request("http://localhost/api/health"));

  expect(response.status).toBe(200);
  expect(response.headers.get("content-type")).toContain("application/json");
  expect(await response.json()).toEqual({ status: "ok", service: "web" });
  expect(response.headers.get("X-Request-ID")).toMatch(/^[a-zA-Z0-9_-]{1,64}$/);
});

it.each(["trace_123-ABC", "a".repeat(64), "invalid request id", "a".repeat(65)])(
  "validates incoming request correlation: %s",
  async (id) => {
    const app = createApp().use(requestId).use("/api/health", health);
    const response = await toWebHandler(app)(
      new Request("http://localhost/api/health", { headers: { "X-Request-ID": id } }),
    );
    const outgoing = response.headers.get("X-Request-ID");
    expect(outgoing).toMatch(/^[a-zA-Z0-9_-]{1,64}$/);
    if (/^[a-zA-Z0-9_-]{1,64}$/.test(id)) expect(outgoing).toBe(id);
    else expect(outgoing).not.toBe(id);
  },
);
