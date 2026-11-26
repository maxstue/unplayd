import { onScopeDispose, ref, shallowRef, watch, type Ref } from "vue";
import type { CatalogPage, SearchHit } from "../../shared/catalog";

type FetchPage = (
  query: string,
  cursor: string | null,
  signal: AbortSignal,
) => Promise<CatalogPage>;

// Each attempt can spend two seconds waiting for the same catalog refresh.
// Seven retries with a one-second delay cover its twenty-second deadline.
const searchRetryLimit = 7;

function isPendingSearch(error: unknown) {
  return (
    error instanceof Error &&
    "response" in error &&
    error.response instanceof Response &&
    error.response.status === 503 &&
    error.response.headers.get("Retry-After") === "1"
  );
}

async function waitForSearchRetry(signal: AbortSignal) {
  signal.throwIfAborted();
  await new Promise<void>((resolve) => {
    const timeout = setTimeout(finish, 1000);
    function finish() {
      clearTimeout(timeout);
      signal.removeEventListener("abort", finish);
      resolve();
    }
    signal.addEventListener("abort", finish, { once: true });
  });
  signal.throwIfAborted();
}

async function fetchSearchPage(fetchPage: FetchPage, term: string, signal: AbortSignal) {
  let retries = 0;
  for (;;) {
    try {
      return await fetchPage(term, null, signal);
    } catch (error) {
      if (!isPendingSearch(error) || retries === searchRetryLimit) {
        throw error;
      }
      retries++;
      await waitForSearchRetry(signal);
    }
  }
}

/**
 * Debounced, cancellable catalog search with cursor pagination. Stale responses never replace newer
 * ones, and results are de-duplicated by game ID.
 *
 * @param query - Search term; 2 to 200 characters after trimming trigger a search.
 * @param enabled - Pauses searching while `false`.
 * @param fetchPage - Loads one {@link CatalogPage}; must honor the given {@link AbortSignal}.
 * @param debounceMs - Delay before searching; `0` searches immediately.
 * @returns Reactive {@link SearchHit} items and status, plus `retry` and `loadMore`.
 */
export function useGameSearch(
  query: Ref<string>,
  enabled: Ref<boolean>,
  fetchPage: FetchPage,
  debounceMs = 300,
) {
  const items = shallowRef<SearchHit[]>([]);
  const nextCursor = ref<string | null>(null);
  const status = ref<"idle" | "loading" | "success" | "error">("idle");
  const loadingMore = ref(false);
  const moreError = ref(false);
  let controller: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let generation = 0;

  function cancel() {
    generation++;
    clearTimeout(timer);
    controller?.abort();
  }

  async function request(append = false) {
    cancel();
    const current = generation;
    const term = query.value.trim();
    if (!enabled.value || term.length < 2 || term.length > 200) {
      return;
    }
    controller = new AbortController();
    const signal = controller.signal;
    if (append) {
      loadingMore.value = true;
    } else {
      status.value = "loading";
    }
    moreError.value = false;
    try {
      const page = append
        ? await fetchPage(term, nextCursor.value, signal)
        : await fetchSearchPage(fetchPage, term, signal);
      // Some transports resolve after abort; they must never replace newer results.
      if (current !== generation) {
        return;
      }
      const combined = append ? [...items.value, ...page.items] : page.items;
      items.value = [...new Map(combined.map((hit) => [hit.game.id, hit])).values()];
      nextCursor.value = page.nextCursor;
      status.value = "success";
    } catch {
      if (current !== generation) {
        return;
      }
      if (append) {
        moreError.value = true;
      } else {
        status.value = "error";
      }
    } finally {
      if (current === generation) {
        loadingMore.value = false;
      }
    }
  }

  watch(
    [query, enabled],
    () => {
      cancel();
      items.value = [];
      nextCursor.value = null;
      moreError.value = false;
      loadingMore.value = false;
      const length = query.value.trim().length;
      status.value = enabled.value && length >= 2 && length <= 200 ? "loading" : "idle";
      if (status.value === "loading") {
        if (debounceMs === 0) {
          void request();
        } else {
          timer = setTimeout(() => void request(), debounceMs);
        }
      }
    },
    { immediate: true, flush: "sync" },
  );
  onScopeDispose(cancel);

  return {
    items,
    nextCursor,
    status,
    loadingMore,
    moreError,
    retry: () => request(),
    loadMore: () => {
      if (!nextCursor.value || loadingMore.value) {
        return;
      }
      return request(true);
    },
  };
}
