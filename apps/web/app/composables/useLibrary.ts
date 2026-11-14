import { computed, onScopeDispose, ref, watch, type Ref } from "vue";
import {
  statusLabels,
  type LibraryEntryView,
  type LibraryFilters,
  type LibraryPageView,
  type PlayStatus,
} from "#shared/library";

export interface LibraryTransport {
  list: (filters: LibraryFilters, signal: AbortSignal) => Promise<LibraryPageView>;
  status: (id: string, status: PlayStatus) => Promise<unknown>;
  favorite: (id: string, favorite: boolean) => Promise<unknown>;
  remove: (id: string) => Promise<unknown>;
}
/** A change to one entry; omitting it removes the entry. */
export type LibraryChange = { status: PlayStatus } | { favorite: boolean };

function confirmation(entry: LibraryEntryView, change?: LibraryChange) {
  const title = `„${entry.display.title}“`;
  if (!change) {
    return `${title} wurde entfernt.`;
  }
  if ("status" in change) {
    return `${title}: ${statusLabels[change.status]} gespeichert.`;
  }
  return change.favorite ? `${title} als Favorit gespeichert.` : `${title} aus Favoriten entfernt.`;
}
/**
 * Loads a library page for the current filters and applies optimistic status, favorite and removal
 * changes. Failed mutations restore the previous page.
 *
 * @param filters - Current {@link LibraryFilters}; changes reload the page.
 * @param transport - {@link LibraryTransport} for listing and mutating entries.
 * @param enabled - Pauses loading while `false`.
 * @returns Reactive {@link LibraryPageView} state plus `refresh` and `mutate`.
 */
export function useLibrary(
  filters: Ref<LibraryFilters>,
  transport: LibraryTransport,
  enabled: Ref<boolean> = ref(true),
) {
  const page = ref<LibraryPageView | null>(null);
  const fetching = ref(true);
  const loading = computed(() => fetching.value && !page.value);
  const refreshing = computed(() => fetching.value && !!page.value);
  const failed = ref(false);
  const pending = ref<string | null>(null);
  const message = ref("");
  const mutationError = ref("");
  let controller: AbortController | undefined;
  let generation = 0;
  let disposed = false;
  async function refresh() {
    controller?.abort();
    const current = ++generation;
    controller = new AbortController();
    fetching.value = true;
    failed.value = false;
    try {
      const result = await transport.list(filters.value, controller.signal);
      if (current === generation && !disposed) {
        page.value = result;
      }
    } catch {
      if (current === generation && !disposed) {
        failed.value = true;
      }
    } finally {
      if (current === generation && !disposed) {
        fetching.value = false;
      }
    }
  }
  watch(
    [filters, enabled],
    () => {
      page.value = null;
      message.value = "";
      mutationError.value = "";
      if (enabled.value) {
        void refresh();
      } else {
        generation++;
        controller?.abort();
        fetching.value = true;
      }
    },
    { immediate: true, deep: true, flush: "sync" },
  );
  onScopeDispose(() => {
    disposed = true;
    generation++;
    controller?.abort();
  });
  async function mutate(id: string, change?: LibraryChange) {
    if (pending.value || fetching.value || !page.value) {
      return;
    }
    const before = page.value;
    const entry = before.items.find((item) => item.gameId === id);
    const unchanged =
      change &&
      ("status" in change ? entry?.status === change.status : entry?.favorite === change.favorite);
    if (!entry || unchanged) {
      return;
    }
    const current = generation;
    pending.value = id;
    mutationError.value = "";
    page.value = {
      ...before,
      items: change
        ? before.items.map((item) => (item.gameId === id ? { ...item, ...change } : item))
        : before.items.filter((item) => item.gameId !== id),
    };
    try {
      if (!change) {
        await transport.remove(id);
      } else if ("status" in change) {
        await transport.status(id, change.status);
      } else {
        await transport.favorite(id, change.favorite);
      }
      if (current === generation && !disposed) {
        message.value = confirmation(entry, change);
      }
      if (!disposed) {
        await refresh();
      }
      return true;
    } catch {
      if (current === generation && !disposed) {
        page.value = before;
        message.value = "";
        mutationError.value =
          "Die Änderung konnte nicht gespeichert werden. Bitte versuche es erneut.";
      }
      return false;
    } finally {
      pending.value = null;
    }
  }
  return { page, loading, refreshing, failed, pending, message, mutationError, refresh, mutate };
}
