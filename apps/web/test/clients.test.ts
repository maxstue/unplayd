import { randomUUID } from "node:crypto";
import { expect, it, vi } from "vite-plus/test";
import { createCatalogClient } from "../server/clients/catalog";
import { createUserClient } from "../server/clients/user";
import { serviceTransport } from "../server/clients/transport";

it("uses typed catalog parameters and forwards server credentials and the request ID", async () => {
  const token = randomUUID();
  const requestId = randomUUID();
  const fetcher = vi
    .fn<typeof fetch>()
    .mockResolvedValue(Response.json({ items: [], nextCursor: null }));
  const client = createCatalogClient({
    baseUrl: "http://catalog-api:8082",
    token,
    requestId,
    fetch: fetcher,
  });
  const result = await client.GET("/games", { params: { query: { query: "Zelda" } } });
  const request = fetcher.mock.calls[0]![0] as Request;

  expect(request.url).toBe("http://catalog-api:8082/games?query=Zelda");
  expect(request.headers.get("Authorization")).toBe(`Bearer ${token}`);
  expect(request.headers.get("X-Request-ID")).toBe(requestId);
  expect(result.data).toEqual({ items: [], nextCursor: null });
});

it("forwards cancellation and represents an empty next-game result without changing status", async () => {
  const controller = new AbortController();
  controller.abort();
  const fetcher = vi.fn<typeof fetch>().mockResolvedValue(Response.json({ entry: null }));
  const client = createUserClient({
    baseUrl: "http://user-api:8081",
    token: randomUUID(),
    requestId: randomUUID(),
    fetch: fetcher,
  });
  const result = await client.GET("/next-game", { signal: controller.signal });

  expect(fetcher).toHaveBeenCalledOnce();
  expect(fetcher.mock.calls[0]![1]?.signal?.aborted).toBe(true);
  expect(result.data).toEqual({ entry: null });
});

it.each([
  { baseUrl: "not-a-url" },
  { baseUrl: "ftp://example.invalid" },
  { baseUrl: `http://${randomUUID()}:${randomUUID()}@example.invalid` },
  { token: "" },
  { requestId: "invalid request id" },
  { timeoutMs: 0 },
])("rejects invalid service configuration without exposing values: %j", (invalid) => {
  expect(() =>
    serviceTransport({
      baseUrl: "http://catalog-api:8082",
      token: randomUUID(),
      requestId: randomUUID(),
      ...invalid,
    }),
  ).toThrow();
});
