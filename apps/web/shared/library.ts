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
export function libraryReturnPath(value: unknown) {
  return typeof value === "string" && /^\/(library|dashboard)(\?|$)/.test(value) ? value : null;
}
export function platformLabel(entry: LibraryEntryView) {
  if (!entry.platformId) {
    return "Ohne Plattform";
  }
  return (
    entry.game?.platforms.find((platform) => platform.id === entry.platformId)?.name ??
    "Plattform nicht verfügbar"
  );
}
