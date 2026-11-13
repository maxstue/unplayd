import { setResponseStatus, type H3Event } from "h3";
import { withServices, upstream, publicError } from "../clients/request";
import type { ServicesConfig } from "../clients/config";
import { statusLabels } from "../../shared/library";
import type { components as Catalog } from "../clients/generated/catalog-api";
import { enrichEntries } from "./library";
import {
  addEntryBody,
  batchBody,
  catalogQuery,
  gameId,
  libraryQuery,
  statusBody,
  suggestionQuery,
} from "./validation";

export function searchGames(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withServices(
    event,
    config,
    async (s) => {
      const query = catalogQuery(event);
      return (
        await upstream(
          () => s.catalog.GET("/games", { params: { query } }),
          "catalog",
          s.requestId,
          { signal: s.signal },
        )
      ).data;
    },
    fetcher,
  );
}
export function getGame(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withServices(
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
export function suggestGames(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withServices(
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
export function batchGames(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withServices(
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
          s.signal.throwIfAborted();
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
export function getLibraryOverview(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withServices(
    event,
    config,
    async (s) => {
      const result = (
        await upstream(() => s.user.GET("/library/overview"), "user", s.requestId, {
          signal: s.signal,
        })
      ).data;
      const counts = Object.keys(statusLabels).map(
        (status) => result.statuses?.[status as keyof typeof statusLabels],
      );
      if (
        !Number.isSafeInteger(result.total) ||
        result.total < 0 ||
        counts.some((count) => !Number.isSafeInteger(count) || count < 0) ||
        counts.reduce((sum, count) => sum + count, 0) !== result.total
      ) {
        throw publicError(503, s.requestId);
      }
      return result;
    },
    fetcher,
  );
}
export function addLibraryEntry(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withServices(
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
export function setLibraryStatus(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withServices(
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
export function deleteLibraryEntry(event: H3Event, config: ServicesConfig, fetcher?: typeof fetch) {
  return withServices(
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
