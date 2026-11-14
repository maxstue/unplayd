import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import type { components } from "../../server/clients/generated/user-api";
import type { GameDetailView } from "../../shared/catalog";

type Entry = components["schemas"]["LibraryEntry"];
const knight = "10000000-0000-4000-8000-000000000001";
const celeste = "10000000-0000-4000-8000-000000000002";
const platform = "20000000-0000-4000-8000-000000000001";
const games: GameDetailView["game"][] = [
  {
    id: knight,
    title: "Hollow Knight",
    coverUrl: null,
    platforms: [{ id: platform, name: "PC" }],
    isActive: true,
    parent: null,
  },
  {
    id: celeste,
    title: "Celeste",
    coverUrl: null,
    platforms: [{ id: platform, name: "PC" }],
    isActive: true,
    parent: null,
  },
];
function entry(gameId: string): Entry {
  return {
    id: gameId,
    gameId,
    platformId: platform,
    ownership: "owned",
    status: "backlog",
    favorite: false,
    rating: null,
    note: null,
    snapshot: { title: games.find((game) => game.id === gameId)!.title, coverUrl: null },
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}
let libraries: Record<string, Entry[]>;
let failLibrary = false;
let failDelete = false;
let holdStatus = false;
let releaseStatus: (() => void) | undefined;
function reset() {
  releaseStatus?.();
  releaseStatus = undefined;
  libraries = { alice: [entry(knight)], bob: [entry(celeste)] };
  failLibrary = false;
  failDelete = false;
  holdStatus = false;
}
reset();

async function body(request: IncomingMessage): Promise<Record<string, unknown>> {
  let raw = "";
  for await (const chunk of request) {
    raw += String(chunk);
  }
  return raw ? (JSON.parse(raw) as Record<string, unknown>) : {};
}
function json(response: ServerResponse, data: unknown, status = 200) {
  response.writeHead(status, { "Content-Type": "application/json" });
  response.end(status === 204 ? undefined : JSON.stringify(data));
}
async function handle(request: IncomingMessage, response: ServerResponse) {
  const url = new URL(request.url!, "http://127.0.0.1:9347");
  if (url.pathname === "/health") {
    return json(response, { ok: true });
  }
  // Control API belongs to this external fixture process, never to the Nuxt app.
  if (url.pathname === "/control/reset") {
    reset();
    return json(response, { ok: true });
  }
  if (url.pathname === "/control/config") {
    const config = await body(request);
    if (typeof config.failLibrary === "boolean") {
      failLibrary = config.failLibrary;
    }
    if (typeof config.failDelete === "boolean") {
      failDelete = config.failDelete;
    }
    if (typeof config.holdStatus === "boolean") {
      holdStatus = config.holdStatus;
    }
    return json(response, { ok: true });
  }
  if (url.pathname === "/control/state") {
    return json(response, { statusPending: Boolean(releaseStatus), libraries });
  }
  if (url.pathname === "/control/release-status") {
    holdStatus = false;
    releaseStatus?.();
    releaseStatus = undefined;
    return json(response, { ok: true });
  }
  if (url.pathname.startsWith("/games")) {
    if (request.headers.authorization !== "Bearer test-catalog-token-only-for-e2e-fixture") {
      return json(response, {}, 401);
    }
    const path = url.pathname;
    if (path === "/games/batch") {
      const { gameIds } = await body(request);
      return json(response, {
        items: games.filter((game) => (gameIds as string[]).includes(game.id)),
        missingGameIds: [],
        inactiveGameIds: [],
      });
    }
    if (path === "/games" || path === "/games/suggestions") {
      const query = url.searchParams.get("query")?.toLowerCase() ?? "";
      return json(response, {
        items: games
          .filter((game) => game.title.toLowerCase().includes(query))
          .map((game) => ({ game, childCounts: [], matchedChildren: [] })),
        nextCursor: null,
      });
    }
    const game = games.find((item) => path === `/games/${item.id}`);
    return json(response, game ? { game, children: [] } : {}, game ? 200 : 404);
  }
  const user = request.headers.authorization?.replace("Bearer ", "") ?? "";
  const entries = libraries[user];
  if (!entries) {
    return json(response, {}, 401);
  }
  const path = url.pathname;
  if (path === "/me") {
    return json(response, { id: user, clerkUserId: user });
  }
  if (path === "/library/overview") {
    const statuses = { backlog: 0, playing: 0, paused: 0, completed: 0, dropped: 0 };
    for (const item of entries) {
      statuses[item.status]++;
    }
    return json(response, { total: entries.length, statuses });
  }
  if (path === "/library/platforms") {
    return json(response, {
      items: entries.length ? [{ platformId: platform, gameId: entries[0]!.gameId }] : [],
    });
  }
  if (path === "/library" && request.method === "GET") {
    if (failLibrary) {
      return json(response, {}, 503);
    }
    return json(response, {
      items: entries.filter(
        (item) =>
          (!url.searchParams.has("status") || item.status === url.searchParams.get("status")) &&
          (!url.searchParams.has("platformId") ||
            item.platformId === url.searchParams.get("platformId")) &&
          (!url.searchParams.has("favorite") || item.favorite),
      ),
      nextCursor: null,
    });
  }
  if (path === "/library/lookup") {
    const { gameIds } = await body(request);
    return json(response, {
      items: entries.filter((item) => (gameIds as string[]).includes(item.gameId)),
    });
  }
  if (path.startsWith("/favorites/")) {
    const gameId = path.split("/")[2]!;
    let favorite = entries.find((item) => item.gameId === gameId);
    if (request.method === "DELETE") {
      if (favorite) {
        favorite.favorite = false;
      }
      return json(response, null, 204);
    }
    if (!favorite) {
      favorite = { ...entry(gameId), ownership: "wishlist", platformId: null };
      entries.push(favorite);
    }
    favorite.favorite = true;
    return json(response, favorite);
  }
  if (path === "/library" && request.method === "POST") {
    const input = await body(request);
    const added = entry(String(input.gameId));
    entries.push(added);
    return json(response, added, 201);
  }
  const id = path.split("/")[2];
  const selected = entries.find((item) => item.gameId === id);
  if (!selected) {
    return json(response, {}, 404);
  }
  if (request.method === "PUT" && path.endsWith("/status")) {
    const input = await body(request);
    if (holdStatus) {
      await new Promise<void>((resolve) => {
        releaseStatus = resolve;
      });
    }
    selected.status = input.status as Entry["status"];
    return json(response, selected);
  }
  if (request.method === "DELETE") {
    if (failDelete) {
      return json(response, {}, 503);
    }
    libraries[user] = entries.filter((item) => item !== selected);
    return json(response, null, 204);
  }
  return json(response, selected);
}
const server = createServer((request, response) => {
  void handle(request, response).catch(() => json(response, { error: "Fixture error" }, 500));
});
server.listen(9347, "127.0.0.1");
process.once("SIGTERM", () => server.close());
