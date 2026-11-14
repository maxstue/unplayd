import { setResponseStatus, type H3Event } from "h3";
import {
  withServices,
  withCatalogService,
  withUserService,
  upstream,
  publicError,
} from "../clients/request";
import type { ServicesConfig } from "../clients/config";
import type { components as Catalog } from "../clients/generated/catalog-api";
import { enrichEntries, withLibraryState } from "./library";
import {
  addEntryBody,
  batchBody,
  catalogQuery,
  gameId,
  libraryQuery,
  statusBody,
  suggestionQuery,
} from "./validation";

/**
 * Searches the catalog with the validated {@link catalogQuery} and adds the user's library state
 * via {@link withLibraryState}.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The catalog search page with `library` state per hit.
 */
export function searchGames(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withCatalogService(
    event,
    config,
    async (s) => {
      const query = catalogQuery(event);
      const page = (
        await upstream(
          () => s.catalog.GET("/games", { params: { query } }),
          "catalog",
          s.requestId,
          { signal: s.signal },
        )
      ).data;
      return withLibraryState(page, event, config, s, fetcher);
    },
    fetcher,
  );
}
/**
 * Loads one catalog game by its `gameId` route parameter.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The catalog game detail.
 */
export function getGame(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withCatalogService(
    event,
    config,
    async (s) => {
      const id = gameId(event);
      return (
        await upstream(
          () => s.catalog.GET("/games/{gameId}", { params: { path: { gameId: id } } }),
          "catalog",
          s.requestId,
          { signal: s.signal },
        )
      ).data;
    },
    fetcher,
  );
}
/**
 * Returns catalog search suggestions for the validated {@link suggestionQuery}.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The catalog suggestions.
 */
export function suggestGames(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withCatalogService(
    event,
    config,
    async (s) => {
      const query = suggestionQuery(event);
      return (
        await upstream(
          () => s.catalog.GET("/games/suggestions", { params: { query } }),
          "catalog",
          s.requestId,
          { signal: s.signal },
        )
      ).data;
    },
    fetcher,
  );
}
/**
 * Looks up several catalog games at once (see {@link batchBody}).
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The catalog batch response.
 */
export function batchGames(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withCatalogService(
    event,
    config,
    async (s) => {
      const body = await batchBody(event);
      return (
        await upstream(() => s.catalog.POST("/games/batch", { body }), "catalog", s.requestId, {
          signal: s.signal,
        })
      ).data;
    },
    fetcher,
  );
}
/**
 * Lists the user's library page, enriched with catalog data via {@link enrichEntries}.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The enriched page with `nextCursor`.
 */
export function listLibrary(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withServices(
    event,
    config,
    async (s) => {
      const query = libraryQuery(event);
      const page = (
        await upstream(
          () =>
            s.user.GET("/library", {
              params: { query },
            }),
          "user",
          s.requestId,
          { signal: s.signal },
        )
      ).data;
      return { ...(await enrichEntries(page.items, s)), nextCursor: page.nextCursor };
    },
    fetcher,
  );
}
/**
 * Loads one library entry, enriched with catalog data via {@link enrichEntries}.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The enriched entry.
 */
export function getLibraryEntry(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withServices(
    event,
    config,
    async (s) => {
      const id = gameId(event);
      const entry = (
        await upstream(
          () =>
            s.user.GET("/library/{gameId}", {
              params: { path: { gameId: id } },
            }),
          "user",
          s.requestId,
          { signal: s.signal },
        )
      ).data;
      return (await enrichEntries([entry], s)).items[0];
    },
    fetcher,
  );
}
/**
 * Lists the platforms used in the library, with names resolved from the catalog.
 * Unresolvable names fall back to a placeholder instead of failing.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns Platforms sorted by German name.
 */
export function listLibraryPlatforms(
  event: H3Event,
  config: ServicesConfig,
  fetcher?: typeof fetch,
) {
  return withServices(
    event,
    config,
    async (s) => {
      const { items } = (
        await upstream(() => s.user.GET("/library/platforms"), "user", s.requestId, {
          signal: s.signal,
        })
      ).data;
      if (!Array.isArray(items)) {
        throw publicError(503, s.requestId);
      }
      const names = new Map<string, string>();
      const ids = [...new Set(items.map((item) => item.gameId))];
      // Only representative games are loaded, in the catalog's bounded batches.
      for (let offset = 0; offset < ids.length; offset += 100) {
        let batch: Catalog["schemas"]["GameBatchResponse"];
        try {
          batch = (
            await upstream(
              () =>
                s.catalog.POST("/games/batch", {
                  body: { gameIds: ids.slice(offset, offset + 100) },
                }),
              "catalog",
              s.requestId,
              { signal: s.signal },
            )
          ).data;
        } catch {
          s.clientSignal.throwIfAborted();
          break;
        }
        if (!Array.isArray(batch.items)) {
          break;
        }
        for (const game of batch.items) {
          for (const platform of game.platforms) {
            names.set(platform.id, platform.name);
          }
        }
      }
      return items
        .map(({ platformId }) => ({
          id: platformId,
          name: names.get(platformId) ?? "Plattform nicht verfügbar",
        }))
        .sort((a, b) => a.name.localeCompare(b.name, "de"));
    },
    fetcher,
  );
}
/**
 * Loads the library overview counts from the user service.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns Total and per-status counts.
 */
export function getLibraryOverview(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withUserService(
    event,
    config,
    async (s) => {
      return (
        await upstream(() => s.user.GET("/library/overview"), "user", s.requestId, {
          signal: s.signal,
        })
      ).data;
    },
    fetcher,
  );
}
/**
 * Adds a library entry from the validated {@link addEntryBody}; mirrors the upstream status.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The created or existing entry.
 */
export function addLibraryEntry(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withUserService(
    event,
    config,
    async (s) => {
      const body = await addEntryBody(event);
      const result = await upstream(() => s.user.POST("/library", { body }), "user", s.requestId, {
        signal: s.signal,
      });
      setResponseStatus(event, result.status);
      return result.data;
    },
    fetcher,
  );
}
/**
 * Changes a library entry's status from the validated {@link statusBody}.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The updated entry.
 */
export function setLibraryStatus(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withUserService(
    event,
    config,
    async (s) => {
      const id = gameId(event);
      const body = await statusBody(event);
      return (
        await upstream(
          () =>
            s.user.PUT("/library/{gameId}/status", {
              params: { path: { gameId: id } },
              body,
            }),
          "user",
          s.requestId,
          { signal: s.signal },
        )
      ).data;
    },
    fetcher,
  );
}
/**
 * Removes a library entry and responds with 204.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns `null`.
 */
export function deleteLibraryEntry(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withUserService(
    event,
    config,
    async (s) => {
      const id = gameId(event);
      await upstream(
        () =>
          s.user.DELETE("/library/{gameId}", {
            params: { path: { gameId: id } },
          }),
        "user",
        s.requestId,
        { allowEmpty: true, signal: s.signal },
      );
      setResponseStatus(event, 204);
      return null;
    },
    fetcher,
  );
}
/**
 * Favorites a game; the user service adds it to the library as wishlist/backlog if absent.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The updated entry.
 */
export function setFavorite(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withUserService(
    event,
    config,
    async (s) => {
      const id = gameId(event);
      return (
        await upstream(
          () => s.user.PUT("/favorites/{gameId}", { params: { path: { gameId: id } } }),
          "user",
          s.requestId,
          { signal: s.signal },
        )
      ).data;
    },
    fetcher,
  );
}
/**
 * Removes a favorite without touching the library entry and responds with 204.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Runtime {@link ServicesConfig} for the upstream services.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns `null`.
 */
export function removeFavorite(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withUserService(
    event,
    config,
    async (s) => {
      const id = gameId(event);
      await upstream(
        () => s.user.DELETE("/favorites/{gameId}", { params: { path: { gameId: id } } }),
        "user",
        s.requestId,
        { allowEmpty: true, signal: s.signal },
      );
      setResponseStatus(event, 204);
      return null;
    },
    fetcher,
  );
}
