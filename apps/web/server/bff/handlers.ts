import { setResponseStatus, type H3Event } from "h3";
import { withServices, upstream, publicError } from "../clients/request";
import type { ServicesConfig } from "../clients/config";
import { statusLabels, type LibraryOverview } from "../../shared/library";
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
        await upstream(() => s.catalog.GET("/games", { params: { query } }), "catalog", s.requestId)
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
        await upstream(() => s.catalog.POST("/games/batch", { body }), "catalog", s.requestId)
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
      const platforms = new Map<string, string>();
      const seen = new Set<string>();
      let cursor: string | undefined;
      do {
        const page = (
          await upstream(
            () => s.user.GET("/library", { params: { query: { limit: 50, cursor } } }),
            "user",
            s.requestId,
          )
        ).data;
        const enriched = await enrichEntries(
          page.items.filter((entry) => entry.platformId && !platforms.has(entry.platformId)),
          s,
        );
        for (const entry of enriched.items) {
          if (!entry.platformId) continue;
          platforms.set(
            entry.platformId,
            entry.game?.platforms.find((p) => p.id === entry.platformId)?.name ??
              "Plattform nicht verfügbar",
          );
        }
        cursor = page.nextCursor ?? undefined;
        if (cursor && seen.has(cursor)) throw publicError(503, s.requestId);
        if (cursor) seen.add(cursor);
      } while (cursor);
      return [...platforms]
        .map(([id, name]) => ({ id, name }))
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
      const result: LibraryOverview = {
        total: 0,
        statuses: { backlog: 0, playing: 0, paused: 0, completed: 0, dropped: 0 },
      };
      const seen = new Set<string>();
      let cursor: string | undefined;
      do {
        const page = (
          await upstream(
            () => s.user.GET("/library", { params: { query: { limit: 50, cursor } } }),
            "user",
            s.requestId,
          )
        ).data;
        if (!Array.isArray(page.items) || page.items.length > 50)
          throw publicError(503, s.requestId);
        for (const entry of page.items) {
          if (!Object.hasOwn(statusLabels, entry.status)) throw publicError(503, s.requestId);
          result.total++;
          result.statuses[entry.status]++;
        }
        cursor = page.nextCursor ?? undefined;
        if (cursor && seen.has(cursor)) throw publicError(503, s.requestId);
        if (cursor) seen.add(cursor);
      } while (cursor);
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
      const result = await upstream(() => s.user.POST("/library", { body }), "user", s.requestId);
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
        true,
      );
      setResponseStatus(event, 204);
      return null;
    },
    fetcher,
  );
}
