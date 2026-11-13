import { randomUUID } from "node:crypto";
import { createServer } from "node:http";
import { once } from "node:events";
import {
  createApp,
  createRouter,
  defineEventHandler,
  getHeader,
  toNodeListener,
  toWebHandler,
} from "h3";
import { afterEach, beforeEach, expect, it, vi } from "vite-plus/test";
import search from "../server/api/games/index.get";
import suggestions from "../server/api/games/suggestions.get";
import detail from "../server/api/games/[gameId].get";
import batch from "../server/api/games/batch.post";
import library from "../server/api/library/index.get";
import platforms from "../server/api/library/platforms.get";
import overview from "../server/api/library/overview.get";
import entry from "../server/api/library/[gameId].get";
import add from "../server/api/library/index.post";
import status from "../server/api/library/[gameId]/status.put";
import remove from "../server/api/library/[gameId].delete";
import requestId from "../server/middleware/request-id";
import { validateServicesConfig } from "../server/clients/config";
import type { components } from "../server/clients/generated/user-api";

const config = {
  appOrigin: "https://app.example",
  userApiUrl: "http://user-api:8445",
  catalogApiUrl: "http://catalog-api:8446",
  catalogServiceToken: randomUUID(),
  serviceTimeoutMs: 1000,
};
vi.mock("#imports", () => ({ useRuntimeConfig: () => config }));
const aliceToken = randomUUID();
const bobToken = randomUUID();
const networkFetch = globalThis.fetch;
const fetcher = vi.fn<typeof fetch>();
const id = randomUUID();
const first = fixture();
const second = fixture();

function fixture(): components["schemas"]["LibraryEntry"] {
  return {
    id: randomUUID(),
    gameId: randomUUID(),
    ownership: "owned",
    platformId: null,
    status: "backlog",
    favorite: false,
    rating: null,
    note: null,
    snapshot: { title: "Stored title", coverUrl: null },
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}
function game(gameId: string) {
  return {
    id: gameId,
    title: "Current catalog title",
    coverUrl: "https://images.example/cover.jpg",
    platforms: [],
    kind: "game",
    parent: null,
    isActive: true,
    updatedAt: "2026-01-02T00:00:00Z",
  };
}
function app() {
  const router = createRouter()
    .get("/api/games", search)
    .get("/api/games/suggestions", suggestions)
    .get("/api/games/:gameId", detail)
    .post("/api/games/batch", batch)
    .get("/api/library", library)
    .get("/api/library/platforms", platforms)
    .get("/api/library/overview", overview)
    .post("/api/library", add)
    .get("/api/library/:gameId", entry)
    .put("/api/library/:gameId/status", status)
    .delete("/api/library/:gameId", remove);
  return createApp()
    .use(requestId)
    .use(
      defineEventHandler((event) => {
        const user = getHeader(event, "x-test-session");
        Object.assign(event.context, {
          auth: () =>
            user === "none"
              ? undefined
              : {
                  isAuthenticated: true,
                  userId: user === "bob" ? "user_bob" : "user_alice",
                  sessionId: "sess_test",
                  getToken: () => Promise.resolve(user === "bob" ? bobToken : aliceToken),
                },
        });
      }),
    )
    .use(router);
}
async function send(
  path: string,
  method = "GET",
  body?: unknown,
  headers?: Record<string, string>,
) {
  return toWebHandler(app())(
    new Request(config.appOrigin + path, {
      method,
      headers: {
        Origin: config.appOrigin,
        "Content-Type": "application/json",
        "X-Request-ID": id,
        ...headers,
      },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    }),
  );
}
function requests() {
  return fetcher.mock.calls.map(([input]) => input as Request);
}

beforeEach(() => {
  vi.spyOn(console, "warn").mockImplementation(() => {});
  config.serviceTimeoutMs = 1000;
  fetcher.mockReset().mockRejectedValue(new Error("Unexpected upstream"));
  vi.stubGlobal("fetch", fetcher);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const emptyOverview = {
  total: 0,
  statuses: { backlog: 0, playing: 0, completed: 0, paused: 0, dropped: 0 },
};

it("loads the complete overview with one user-service query and no catalog requests", async () => {
  const summary = {
    ...emptyOverview,
    total: 125,
    statuses: { ...emptyOverview.statuses, playing: 125 },
  };
  fetcher.mockResolvedValueOnce(Response.json(summary));
  const response = await send("/api/library/overview");
  expect(response.status).toBe(200);
  expect(await response.json()).toEqual(summary);
  expect(fetcher).toHaveBeenCalledOnce();
  expect(requests()[0]!.url).toBe(`${config.userApiUrl}/library/overview`);
  expect(requests()[0]!.headers.get("Authorization")).toBe(`Bearer ${aliceToken}`);
});

it("reports an empty overview and rejects unsigned sessions without upstream requests", async () => {
  expect(
    (await send("/api/library/overview", "GET", undefined, { "x-test-session": "none" })).status,
  ).toBe(401);
  expect(fetcher).not.toHaveBeenCalled();
  fetcher.mockResolvedValueOnce(Response.json(emptyOverview));
  expect(await (await send("/api/library/overview")).json()).toEqual(emptyOverview);
});

it.each([
  {},
  { ...emptyOverview, total: -1 },
  { ...emptyOverview, total: 5 },
  { ...emptyOverview, statuses: { ...emptyOverview.statuses, backlog: "0" } },
])("refuses malformed aggregate responses: %j", async (summary) => {
  fetcher.mockResolvedValueOnce(Response.json(summary));
  expect((await send("/api/library/overview")).status).toBe(503);
});

it("loads only representative games for distinct assigned platforms", async () => {
  const pc = randomUUID();
  const consoleId = randomUUID();
  fetcher
    .mockResolvedValueOnce(
      Response.json({
        items: [
          { platformId: pc, gameId: first.gameId },
          { platformId: consoleId, gameId: second.gameId },
        ],
      }),
    )
    .mockResolvedValueOnce(
      Response.json({
        items: [
          {
            ...game(first.gameId),
            platforms: [
              { id: pc, name: "PC" },
              { id: consoleId, name: "Konsole" },
            ],
          },
        ],
        missingGameIds: [second.gameId],
        inactiveGameIds: [],
      }),
    );
  expect(await (await send("/api/library/platforms")).json()).toEqual([
    { id: consoleId, name: "Konsole" },
    { id: pc, name: "PC" },
  ]);
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(requests()[0]!.url).toBe(`${config.userApiUrl}/library/platforms`);
  expect(await requests()[1]!.json()).toEqual({ gameIds: [first.gameId, second.gameId] });
});

it("retains platform choices when catalog names cannot be loaded and skips empty batches", async () => {
  const platformId = randomUUID();
  fetcher
    .mockResolvedValueOnce(Response.json({ items: [{ gameId: first.gameId, platformId }] }))
    .mockResolvedValueOnce(new Response(null, { status: 503 }));
  expect(await (await send("/api/library/platforms")).json()).toEqual([
    { id: platformId, name: "Plattform nicht verfügbar" },
  ]);
  fetcher.mockReset().mockResolvedValueOnce(Response.json({ items: [] }));
  expect(await (await send("/api/library/platforms")).json()).toEqual([]);
  expect(fetcher).toHaveBeenCalledOnce();
});

it("bounds each catalog batch when a library has more than 100 platforms", async () => {
  const items = Array.from({ length: 101 }, () => ({
    platformId: randomUUID(),
    gameId: randomUUID(),
  }));
  fetcher
    .mockResolvedValueOnce(Response.json({ items }))
    .mockImplementation(async () =>
      Response.json({ items: [], missingGameIds: [], inactiveGameIds: [] }),
    );
  expect(await (await send("/api/library/platforms")).json()).toHaveLength(101);
  expect(fetcher).toHaveBeenCalledTimes(3);
  const sizes = await Promise.all(
    requests()
      .slice(1)
      .map(async (request) => ((await request.json()) as { gameIds: string[] }).gameIds.length),
  );
  expect(sizes).toEqual([100, 1]);
});

it("requires a session for platform choices", async () => {
  expect(
    (await send("/api/library/platforms", "GET", undefined, { "x-test-session": "none" })).status,
  ).toBe(401);
  expect(fetcher).not.toHaveBeenCalled();
});

it("aggregates a page with one batch and forwards only the appropriate credentials and filters", async () => {
  fetcher.mockImplementation(async (input) => {
    const request = input as Request;
    if (request.url.startsWith(config.userApiUrl)) {
      return Response.json({ items: [first, second], nextCursor: "next" });
    }
    expect(await request.json()).toEqual({ gameIds: [first.gameId, second.gameId] });
    return Response.json({
      items: [game(first.gameId)],
      missingGameIds: [second.gameId],
      inactiveGameIds: [],
    });
  });
  const response = await send(
    "/api/library?status=backlog&ownership=owned&limit=2&cursor=opaque&userId=forged",
    "GET",
    undefined,
    {
      Authorization: `Bearer ${randomUUID()}`,
      Cookie: `private=${randomUUID()}`,
      "X-User-ID": "forged",
    },
  );
  expect(response.status).toBe(200);
  expect(await response.json()).toMatchObject({
    nextCursor: "next",
    catalogUnavailable: false,
    items: [
      {
        gameId: first.gameId,
        game: game(first.gameId),
        display: { title: "Current catalog title" },
        catalogStatus: "available",
      },
      { gameId: second.gameId, game: null, display: second.snapshot, catalogStatus: "missing" },
    ],
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  const [user, catalog] = requests();
  expect(user!.url).toBe(
    `${config.userApiUrl}/library?status=backlog&ownership=owned&limit=2&cursor=opaque`,
  );
  expect(user!.headers.get("Authorization")).toBe(`Bearer ${aliceToken}`);
  expect(catalog!.url).toBe(`${config.catalogApiUrl}/games/batch`);
  expect(catalog!.headers.get("Authorization")).toBe(`Bearer ${config.catalogServiceToken}`);
  for (const request of requests()) {
    expect(request.headers.get("X-Request-ID")).toBe(id);
    expect(request.headers.has("X-User-ID")).toBe(false);
    expect(request.headers.has("Cookie")).toBe(false);
  }
  expect(response.headers.get("Cache-Control")).toBe("no-store");
});

it("keeps simultaneous user sessions separate and skips catalog requests for empty pages", async () => {
  fetcher.mockImplementation(async (input) => {
    const request = input as Request;
    expect(request.url.startsWith(config.userApiUrl)).toBe(true);
    const ownCursor =
      request.headers.get("Authorization") === `Bearer ${aliceToken}` ? "alice" : "bob";
    return Response.json({ items: [], nextCursor: ownCursor });
  });
  const responses = await Promise.all([
    send("/api/library"),
    send("/api/library", "GET", undefined, { "X-Test-Session": "bob" }),
  ]);
  expect(await responses[0]!.json()).toEqual({
    items: [],
    nextCursor: "alice",
    catalogUnavailable: false,
  });
  expect(await responses[1]!.json()).toEqual({
    items: [],
    nextCursor: "bob",
    catalogUnavailable: false,
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
});

it.each([401, 403, 429, 500, 503, "invalid-json", "invalid-shape"])(
  "falls back to snapshots with an explicit indication for catalog failure %s",
  async (failure) => {
    fetcher.mockResolvedValueOnce(Response.json({ items: [first], nextCursor: null }));
    fetcher.mockResolvedValueOnce(
      typeof failure === "number"
        ? Response.json(
            { secret: config.catalogServiceToken, url: config.catalogApiUrl },
            { status: failure },
          )
        : new Response(failure === "invalid-json" ? "broken" : "{}", {
            headers: { "Content-Type": "application/json" },
          }),
    );
    const response = await send("/api/library");
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({
      items: [
        {
          gameId: first.gameId,
          platformId: first.platformId,
          ownership: first.ownership,
          status: first.status,
          game: null,
          display: first.snapshot,
          catalogStatus: "unavailable",
        },
      ],
      nextCursor: null,
      catalogUnavailable: true,
    });
  },
);

it("uses the snapshot for an inactive game and enriches individual library entries", async () => {
  fetcher
    .mockResolvedValueOnce(Response.json(first))
    .mockResolvedValueOnce(
      Response.json({ items: [], missingGameIds: [], inactiveGameIds: [first.gameId] }),
    );
  const response = await send(`/api/library/${first.gameId}`);
  expect(response.status).toBe(200);
  expect(await response.json()).toMatchObject({
    gameId: first.gameId,
    display: first.snapshot,
    catalogStatus: "inactive",
  });
});

it.each([400, 401, 403, 404, 409, 429, 500, 503])(
  "translates user-service errors %s without reflecting upstream data",
  async (statusCode) => {
    fetcher.mockResolvedValueOnce(
      Response.json(
        { error: { message: `${config.userApiUrl} ${aliceToken}` } },
        { status: statusCode },
      ),
    );
    const response = await send("/api/library");
    expect(response.status).toBe([429, 500].includes(statusCode) ? 503 : statusCode);
    const text = await response.text();
    expect(text).not.toContain(config.userApiUrl);
    expect(text).not.toContain(aliceToken);
    expect(text).toContain(id);
    expect(fetcher).toHaveBeenCalledOnce();
  },
);

it("routes search, detail and explicit batch directly to Go with validated parameters", async () => {
  fetcher.mockImplementation(async () => Response.json({ items: [], nextCursor: null }));
  expect(
    (await send("/api/games?query=Zelda&kind=dlc&kind=expansion&limit=10&userId=forged")).status,
  ).toBe(200);
  expect(requests()[0]!.url).toBe(
    `${config.catalogApiUrl}/games?query=Zelda&limit=10&kind=dlc&kind=expansion`,
  );
  expect((await send(`/api/games/${first.gameId}`)).status).toBe(200);
  expect(requests()[1]!.url).toBe(`${config.catalogApiUrl}/games/${first.gameId}`);
  expect((await send("/api/games/batch", "POST", { gameIds: [first.gameId] })).status).toBe(200);
  expect(await requests()[2]!.json()).toEqual({ gameIds: [first.gameId] });
  expect(
    requests().every(
      (request) => request.headers.get("Authorization") === `Bearer ${config.catalogServiceToken}`,
    ),
  ).toBe(true);
});

it.each([200, 201])(
  "preserves Ktor create semantics with status %s and never performs a catalog write",
  async (statusCode) => {
    fetcher.mockResolvedValueOnce(Response.json(first, { status: statusCode }));
    const response = await send("/api/library", "POST", {
      gameId: first.gameId,
      ownership: "owned",
      platformId: null,
    });
    expect(response.status).toBe(statusCode);
    expect(await response.json()).toEqual(first);
    expect(await requests()[0]!.json()).toEqual({
      gameId: first.gameId,
      ownership: "owned",
      platformId: null,
    });
    expect(requests()[0]!.url).toBe(`${config.userApiUrl}/library`);
    expect(fetcher).toHaveBeenCalledOnce();
  },
);

it("forwards only status to Ktor and returns an empty 204 on deletion", async () => {
  fetcher
    .mockResolvedValueOnce(Response.json({ ...first, status: "playing" }))
    .mockResolvedValueOnce(new Response(null, { status: 204 }));
  expect(
    (await send(`/api/library/${first.gameId}/status`, "PUT", { status: "playing" })).status,
  ).toBe(200);
  expect(await requests()[0]!.json()).toEqual({ status: "playing" });
  const response = await send(`/api/library/${first.gameId}`, "DELETE");
  expect(response.status).toBe(204);
  expect(await response.text()).toBe("");
  expect(requests()[1]!.method).toBe("DELETE");
});

it.each([
  ["/api/library?limit=51", "GET"],
  ["/api/library?limit=2&limit=3", "GET"],
  ["/api/library?status=unknown", "GET"],
  ["/api/games?query=x", "GET"],
  ["/api/games?query=ok&kind=bad", "GET"],
  ["/api/games?query=ok&kind=dlc&kind=dlc", "GET"],
  ["/api/games/invalid", "GET"],
  ["/api/library", "POST", { gameId: first.gameId, ownership: "owned", userId: "forged" }],
  ["/api/library", "POST", { gameId: first.gameId, ownership: "bad" }],
  ["/api/library", "POST", {}],
  [`/api/library/${first.gameId}/status`, "PUT", { status: "bad" }],
  ["/api/games/batch", "POST", { gameIds: [] }],
  ["/api/games/batch", "POST", { gameIds: [first.gameId, first.gameId] }],
])(
  "rejects invalid input on %s %s before any upstream request",
  async (path, method, body?: unknown) => {
    const response = await send(path, method, body);
    expect(response.status).toBe(400);
    expect(await response.text()).toContain(id);
    expect(fetcher).not.toHaveBeenCalled();
  },
);

it("rejects missing sessions and cross-origin writes before upstream requests", async () => {
  expect((await send("/api/library", "GET", undefined, { "X-Test-Session": "none" })).status).toBe(
    401,
  );
  expect(
    (await send("/api/library", "POST", {}, { Origin: "https://foreign.example" })).status,
  ).toBe(403);
  expect(fetcher).not.toHaveBeenCalled();
});

it("aborts a slow catalog request on timeout and uses snapshots", async () => {
  config.serviceTimeoutMs = 30;
  let aborted = false;
  fetcher
    .mockResolvedValueOnce(Response.json({ items: [first], nextCursor: null }))
    .mockImplementationOnce(
      (_input, init) =>
        new Promise((_resolve, reject) => {
          init!.signal!.addEventListener(
            "abort",
            () => {
              aborted = true;
              reject(new Error("Timed out"));
            },
            { once: true },
          );
        }),
    );
  const response = await send("/api/library");
  expect(response.status).toBe(200);
  expect(aborted).toBe(true);
  expect(await response.json()).toMatchObject({ catalogUnavailable: true });
});

it.each(["user", "catalog"])(
  "cancels the in-flight %s request when the browser closes its real HTTP connection",
  async (phase) => {
    let started!: () => void;
    let aborted!: () => void;
    const start = new Promise<void>((resolve) => {
      started = resolve;
    });
    const abort = new Promise<void>((resolve) => {
      aborted = resolve;
    });
    if (phase === "catalog") {
      fetcher.mockResolvedValueOnce(Response.json({ items: [first], nextCursor: null }));
    }
    fetcher.mockImplementation(
      (_input, init) =>
        new Promise((_resolve, reject) => {
          started();
          init!.signal!.addEventListener(
            "abort",
            () => {
              aborted();
              reject(new Error("Cancelled"));
            },
            { once: true },
          );
        }),
    );
    const server = createServer(toNodeListener(app()));
    server.listen(0, "127.0.0.1");
    await once(server, "listening");
    const address = server.address();
    if (!address || typeof address === "string") {
      throw new Error("Missing test port");
    }
    const controller = new AbortController();
    try {
      const pending = networkFetch(`http://127.0.0.1:${address.port}/api/library`, {
        signal: controller.signal,
      });
      const rejected = expect(pending).rejects.toThrow();
      await start;
      controller.abort();
      await rejected;
      await abort;
      expect(fetcher).toHaveBeenCalledTimes(phase === "user" ? 1 : 2);
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      );
    }
  },
);

it("refuses an oversized upstream page before sending an unbounded catalog batch", async () => {
  fetcher.mockResolvedValueOnce(
    Response.json({ items: Array.from({ length: 51 }, fixture), nextCursor: null }),
  );
  const response = await send("/api/library");
  expect(response.status).toBe(503);
  expect(fetcher).toHaveBeenCalledOnce();
});

it("rejects malformed JSON without forwarding or reflecting the body", async () => {
  const response = await toWebHandler(app())(
    new Request(config.appOrigin + "/api/library", {
      method: "POST",
      headers: { Origin: config.appOrigin, "Content-Type": "application/json" },
      body: `{${aliceToken}`,
    }),
  );
  expect(response.status).toBe(400);
  expect(await response.text()).not.toContain(aliceToken);
  expect(fetcher).not.toHaveBeenCalled();
});

it.each([
  { userApiUrl: "not-a-url" },
  { catalogApiUrl: `https://${randomUUID()}@catalog.example` },
  { catalogApiUrl: "https://catalog.example/path" },
  { catalogApiUrl: "https://catalog.example?secret=value" },
  { catalogServiceToken: "" },
  { serviceTimeoutMs: 0 },
  { serviceTimeoutMs: "invalid" },
  { serviceTimeoutMs: 30001 },
])("rejects invalid service configuration without reflecting the value: %j", (invalid) => {
  expect(() => validateServicesConfig({ ...config, ...invalid })).toThrow(/NUXT_/);
});

it("suggests games through the catalog route without touching user data", async () => {
  fetcher.mockResolvedValueOnce(Response.json({ items: [], nextCursor: null }));
  const response = await send("/api/games/suggestions?query=Hollow");
  expect(response.status).toBe(200);
  expect(await response.json()).toEqual({ items: [], nextCursor: null });
  expect(requests()).toHaveLength(1);
  expect(requests()[0]?.url).toBe(config.catalogApiUrl + "/games/suggestions?query=Hollow");
  expect(requests()[0]?.headers.get("Authorization")).toBe("Bearer " + config.catalogServiceToken);
});

it("rejects invalid and signed-out suggestion requests before calling the catalog", async () => {
  for (const query of [
    "",
    "query=x",
    "query=Hollow&query=Hades",
    "query=Hollow&limit=50",
    "query=" + "x".repeat(201),
  ]) {
    expect((await send("/api/games/suggestions?" + query)).status).toBe(400);
  }
  expect(
    (
      await send("/api/games/suggestions?query=Hollow", "GET", undefined, {
        "x-test-session": "none",
      })
    ).status,
  ).toBe(401);
  expect(fetcher).not.toHaveBeenCalled();
});
