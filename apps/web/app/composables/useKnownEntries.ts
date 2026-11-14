/**
 * Shared state of game IDs the session has seen in or removed from the library.
 *
 * @returns A `useState` ref mapping game ID to whether it is in the library.
 */
export function useKnownEntries() {
  return useState<Record<string, boolean>>("known-library-entries", () => ({}));
}
