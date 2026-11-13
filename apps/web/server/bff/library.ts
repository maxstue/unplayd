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
    services.signal.throwIfAborted();
  }
  const games = new Map(batch?.items.map((game) => [game.id, game]));
  return {
    items: entries.map((entry) => {
      const game = games.get(entry.gameId) ?? null;
      const catalogStatus = getCatalogStatus(batch, entry.gameId, Boolean(game));
      return {
        ...entry,
        game,
        display: game ? { title: game.title, coverUrl: game.coverUrl } : entry.snapshot,
        catalogStatus,
      };
    }),
    catalogUnavailable: !batch,
  };
}
