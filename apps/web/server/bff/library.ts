import type { H3Event } from "h3";
import type { components } from "../clients/generated/user-api";
import type { components as Catalog } from "../clients/generated/catalog-api";
import { abortable, upstream, publicError, type RequestServices } from "../clients/request";
import { requireSession } from "../auth/session";
import { validateServicesConfig, type ServicesConfig } from "../clients/config";
import { createUserClient } from "../clients/user";

type Entry = components["schemas"]["LibraryEntry"];
type Batch = Catalog["schemas"]["GameBatchResponse"];
type SearchPage = Catalog["schemas"]["GamePage"];

// Search results must not wait long for optional library state.
const lookupTimeoutMs = 500;

function getCatalogStatus(batch: Batch | undefined, gameId: string, hasGame: boolean) {
  if (!batch) {
    return "unavailable";
  }
  if (batch.inactiveGameIds.includes(gameId)) {
    return "inactive";
  }
  return hasGame ? "available" : "missing";
}

/**
 * Joins library entries with current catalog data. If the catalog fails within the deadline,
 * entries fall back to their stored snapshots and `catalogUnavailable` is set.
 *
 * @param entries - Up to 50 user-service {@link Entry} items.
 * @param services - {@link RequestServices} for the current request.
 * @returns View items with `display` data and a per-entry `catalogStatus`.
 */
export async function enrichEntries(entries: Entry[], services: RequestServices) {
  if (!Array.isArray(entries) || entries.length > 50) {
    throw publicError(503, services.requestId);
  }
  if (!entries.length) {
    return { items: [], catalogUnavailable: false };
  }
  let batch: Batch | undefined;
  try {
    batch = (
      await upstream(
        () =>
          services.catalog.POST("/games/batch", {
            body: { gameIds: [...new Set(entries.map((entry) => entry.gameId))] },
          }),
        "catalog",
        services.requestId,
        { signal: services.signal },
      )
    ).data;
    if (
      !Array.isArray(batch.items) ||
      !Array.isArray(batch.missingGameIds) ||
      !Array.isArray(batch.inactiveGameIds)
    ) {
      batch = undefined;
    }
  } catch {
    // A spent aggregation budget still permits the already loaded snapshots.
    services.clientSignal.throwIfAborted();
  }
  const games = new Map(batch?.items.map((game) => [game.id, game]));
  return {
    items: entries.map((entry) => {
      const game = games.get(entry.gameId) ?? null;
      const catalogStatus = getCatalogStatus(batch, entry.gameId, Boolean(game));
      return {
        gameId: entry.gameId,
        platformId: entry.platformId,
        ownership: entry.ownership,
        status: entry.status,
        favorite: entry.favorite,
        game,
        display: game ? { title: game.title, coverUrl: game.coverUrl } : entry.snapshot,
        catalogStatus,
      };
    }),
    catalogUnavailable: !batch,
  };
}
/**
 * Adds the user's library state to search hits. The lookup is optional: without a token, after
 * {@link lookupTimeoutMs} or on any user-service failure, hits keep `library: null` (unknown).
 *
 * @param page - The catalog search page.
 * @param event - The incoming {@link H3Event}, used for the session token.
 * @param config - Runtime {@link ServicesConfig}.
 * @param services - Request ID and signals of the search request.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The page whose hits carry `library: { status, favorite } | null`.
 */
export async function withLibraryState(
  page: SearchPage,
  event: H3Event,
  config: ServicesConfig,
  services: Pick<RequestServices, "requestId" | "signal" | "clientSignal">,
  fetcher?: typeof fetch,
) {
  let entries: Map<string, Entry> | undefined;
  if (page.items.length) {
    const signal = AbortSignal.any([services.signal, AbortSignal.timeout(lookupTimeoutMs)]);
    try {
      const token = await abortable<string | null>(
        requireSession(event, config.appOrigin).getToken(),
        signal,
      );
      if (token) {
        const user = createUserClient({
          requestId: services.requestId,
          timeoutMs: validateServicesConfig(config).serviceTimeoutMs,
          signal,
          fetch: fetcher,
          baseUrl: config.userApiUrl,
          token,
        });
        const { items } = (
          await upstream(
            () =>
              user.POST("/library/lookup", {
                body: { gameIds: [...new Set(page.items.map((hit) => hit.game.id))] },
              }),
            "user",
            services.requestId,
            { signal },
          )
        ).data;
        if (Array.isArray(items)) {
          entries = new Map(items.map((entry) => [entry.gameId, entry]));
        }
      }
    } catch {
      services.clientSignal.throwIfAborted();
    }
  }
  return {
    ...page,
    items: page.items.map((hit) => {
      if (!entries) {
        return { ...hit, library: null };
      }
      const entry = entries.get(hit.game.id);
      return {
        ...hit,
        library: { status: entry?.status ?? null, favorite: entry?.favorite ?? false },
      };
    }),
  };
}
