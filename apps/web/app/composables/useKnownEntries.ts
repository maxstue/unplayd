// Game IDs the session has seen in or removed from the library.
export function useKnownEntries() {
  return useState<Record<string, boolean>>("known-library-entries", () => ({}));
}
