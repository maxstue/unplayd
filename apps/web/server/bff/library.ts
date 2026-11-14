import type { components } from "../clients/generated/user-api";
import type { components as Catalog } from "../clients/generated/catalog-api";
import { upstream, publicError, type RequestServices } from "../clients/request";

type Entry = components["schemas"]["LibraryEntry"];
type Batch = Catalog["schemas"]["GameBatchResponse"];

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
        game,
        display: game ? { title: game.title, coverUrl: game.coverUrl } : entry.snapshot,
        catalogStatus,
      };
    }),
    catalogUnavailable: !batch,
  };
}
