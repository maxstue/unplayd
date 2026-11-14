import type { GameCardModel } from "./catalog";

export const statusLabels = {
  backlog: "Backlog",
  playing: "Spiele ich gerade",
  paused: "Pausiert",
  completed: "Abgeschlossen",
  dropped: "Abgebrochen",
};
export type PlayStatus = keyof typeof statusLabels;
export const ownershipLabels = {
  owned: "In Besitz",
  wishlist: "Wunschliste",
  subscription: "Abonnement",
};
export interface LibraryEntryView {
  gameId: string;
  platformId: string | null;
  ownership: keyof typeof ownershipLabels;
  status: PlayStatus;
  game: GameCardModel | null;
  display: { title: string; coverUrl: string | null };
  catalogStatus: "available" | "unavailable" | "inactive" | "missing";
}
export interface LibraryPageView {
  items: LibraryEntryView[];
  nextCursor: string | null;
  catalogUnavailable: boolean;
}
export interface LibraryOverview {
  total: number;
  statuses: Record<PlayStatus, number>;
}
export interface LibraryFilters {
  status?: PlayStatus;
  platformId?: string;
  cursor?: string;
}
/**
 * Picks valid library filters from a route query and drops everything else.
 *
 * @param query - Raw route query.
 * @returns The valid {@link LibraryFilters}.
 */
export function libraryFilters(query: Record<string, unknown>): LibraryFilters {
  return {
    ...(typeof query.status === "string" && Object.hasOwn(statusLabels, query.status)
      ? { status: query.status as PlayStatus }
      : {}),
    ...(typeof query.platformId === "string" &&
    /^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(query.platformId)
      ? { platformId: query.platformId }
      : {}),
    ...(typeof query.cursor === "string" && query.cursor.length > 0 && query.cursor.length <= 512
      ? { cursor: query.cursor }
      : {}),
  };
}
/**
 * Accepts only internal library or dashboard paths as a return target, preventing open redirects.
 *
 * @param value - Untrusted return path, e.g. from a query parameter.
 * @returns The path, or `null` if it is not allowed.
 */
export function libraryReturnPath(value: unknown) {
  return typeof value === "string" && /^\/(library|dashboard)(\?|$)/.test(value) ? value : null;
}
/**
 * Returns the display name of an entry's platform.
 *
 * @param entry - The {@link LibraryEntryView}.
 * @returns The platform name, or a German placeholder if none is set or it cannot be resolved.
 */
export function platformLabel(entry: LibraryEntryView) {
  if (!entry.platformId) {
    return "Ohne Plattform";
  }
  return (
    entry.game?.platforms.find((platform) => platform.id === entry.platformId)?.name ??
    "Plattform nicht verfügbar"
  );
}
