import type { GameCardModel } from "./catalog";

export const statusLabels = {
  backlog: "Backlog",
  playing: "Spiele ich gerade",
  paused: "Pausiert",
  completed: "Abgeschlossen",
  dropped: "Abgebrochen",
};
export type PlayStatus = keyof typeof statusLabels;
/** A game's state in the user's library; `status` is `null` when the game is not in the library. */
export interface LibraryState {
  status: PlayStatus | null;
  favorite: boolean;
}
/** Maximum note length in Unicode code points, matching the user service. */
export const noteMaxLength = 10_000;
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
  favorite: boolean;
  rating: number | null;
  note: string | null;
  game: GameCardModel | null;
  display: { title: string; coverUrl: string | null };
  catalogStatus: "available" | "unavailable" | "inactive" | "missing";
}
export interface LibraryPageView {
  items: LibraryEntryView[];
  nextCursor: string | null;
  catalogUnavailable: boolean;
}
/** A random backlog draw; `entry` is `null` when no backlog entry matches the filter. */
export interface NextGameView {
  entry: LibraryEntryView | null;
  catalogUnavailable: boolean;
}
export interface LibraryOverview {
  total: number;
  statuses: Record<PlayStatus, number>;
}
export interface LibraryFilters {
  status?: PlayStatus;
  platformId?: string;
  favorite?: "true";
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
    ...(query.favorite === "true" ? { favorite: "true" as const } : {}),
    ...(typeof query.cursor === "string" && query.cursor.length > 0 && query.cursor.length <= 512
      ? { cursor: query.cursor }
      : {}),
  };
}
/**
 * Accepts only internal library, dashboard or next-game paths as a return target, preventing open redirects.
 *
 * @param value - Untrusted return path, e.g. from a query parameter.
 * @returns The path, or `null` if it is not allowed.
 */
export function libraryReturnPath(value: unknown) {
  return typeof value === "string" && /^\/(library|dashboard|next-game)(\?|$)/.test(value)
    ? value
    : null;
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
