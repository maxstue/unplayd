import { onScopeDispose, ref, shallowRef, watch, type Ref } from "vue";
import type { CatalogPage, SearchHit } from "../../shared/catalog";

type FetchPage = (
  query: string,
  cursor: string | null,
  signal: AbortSignal,
) => Promise<CatalogPage>;

export function useGameSearch(query: Ref<string>, enabled: Ref<boolean>, fetchPage: FetchPage) {
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
    if (!enabled.value || term.length < 2 || term.length > 200) return;
    controller = new AbortController();
    if (append) loadingMore.value = true;
    else status.value = "loading";
    moreError.value = false;
    try {
      const page = await fetchPage(term, append ? nextCursor.value : null, controller.signal);
      // Some transports resolve after abort; they must never replace newer results.
      if (current !== generation) return;
      const combined = append ? [...items.value, ...page.items] : page.items;
      items.value = [...new Map(combined.map((hit) => [hit.game.id, hit])).values()];
      nextCursor.value = page.nextCursor;
      status.value = "success";
    } catch {
      if (current !== generation) return;
      if (append) moreError.value = true;
      else status.value = "error";
    } finally {
      if (current === generation) loadingMore.value = false;
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
      if (status.value === "loading") timer = setTimeout(() => void request(), 300);
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
      if (!nextCursor.value || loadingMore.value) return;
      return request(true);
    },
  };
}
