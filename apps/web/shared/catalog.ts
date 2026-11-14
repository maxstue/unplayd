// Browser view models. Service credentials and clients stay in server/.
import type { LibraryState } from "./library";

export interface GameCardModel {
  id: string;
  title: string;
  coverUrl: string | null;
  platforms: { id: string; name: string }[];
}

export interface SearchHit {
  game: GameCardModel;
  childCounts: { kind: string; count: number }[];
  matchedChildren: GameCardModel[];
  /** The user's state for this game; `null` when it could not be loaded. */
  library: LibraryState | null;
}

export interface GameDetailView {
  game: GameCardModel & { isActive: boolean; parent: { id: string; title: string } | null };
  children: { kind: string; games: GameCardModel[] }[];
}

export interface CatalogPage {
  items: SearchHit[];
  nextCursor: string | null;
}

export const kindLabels: Record<string, string> = {
  game: "Hauptspiel",
  edition: "Editionen",
  dlc: "DLCs",
  expansion: "Erweiterungen",
  standalone_expansion: "Eigenständige Erweiterungen",
  pack: "Packs",
  update: "Updates",
  bundle: "Bundles",
  remake: "Remakes",
  remaster: "Remaster",
  expanded_game: "Erweiterte Editionen",
  port: "Ports",
  mod: "Mods",
  fork: "Abwandlungen",
  episode: "Episoden",
  season: "Staffeln",
};
