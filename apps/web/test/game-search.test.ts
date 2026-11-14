import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { effectScope, ref } from "vue";
import { useGameSearch } from "../app/composables/useGameSearch";
import type { CatalogPage } from "../shared/catalog";

function page(id: string, nextCursor: string | null = null): CatalogPage {
  return {
    items: [
      {
        game: { id, title: id, coverUrl: null, platforms: [] },
        childCounts: [],
        matchedChildren: [],
        library: null,
      },
    ],
    nextCursor,
  };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function setup(fetchPage: Parameters<typeof useGameSearch>[2]) {
  const scope = effectScope();
  const query = ref("");
  const enabled = ref(true);
  const search = scope.run(() => useGameSearch(query, enabled, fetchPage))!;
  scopes.push(scope);
  return { query, enabled, search, scope };
}
const scopes: ReturnType<typeof effectScope>[] = [];
beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  scopes.splice(0).forEach((scope) => scope.stop());
  vi.useRealTimers();
});

describe("catalog search lifecycle", () => {
  it("debounces input and ignores old results even when a transport resolves after abort", async () => {
    const old = deferred<CatalogPage>();
    const fetcher = vi
      .fn<Parameters<typeof useGameSearch>[2]>()
      .mockReturnValueOnce(old.promise)
      .mockResolvedValue(page("new"));
    const { query, search } = setup(fetcher);
    query.value = "Hol";
    await vi.advanceTimersByTimeAsync(200);
    query.value = "Hollow";
    await vi.advanceTimersByTimeAsync(299);
    expect(fetcher).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    const signal = fetcher.mock.calls[0]![2];
    query.value = "Hades";
    expect(signal.aborted).toBe(true);
    expect(search.items.value).toEqual([]);
    await vi.advanceTimersByTimeAsync(300);
    old.resolve(page("stale"));
    await Promise.resolve();
    expect(search.items.value[0]?.game.id).toBe("new");
    expect(search.status.value).toBe("success");
  });

  it("never sends short, oversized or signed-out searches and cancels on logout", async () => {
    const pending = deferred<CatalogPage>();
    const fetcher = vi.fn<Parameters<typeof useGameSearch>[2]>().mockReturnValue(pending.promise);
    const { query, enabled, search } = setup(fetcher);
    for (const invalid of [" ", "x", "x".repeat(201)]) {
      query.value = invalid;
      await vi.advanceTimersByTimeAsync(300);
    }
    enabled.value = false;
    query.value = "Hollow Knight";
    await vi.advanceTimersByTimeAsync(300);
    expect(fetcher).not.toHaveBeenCalled();
    enabled.value = true;
    await vi.advanceTimersByTimeAsync(300);
    enabled.value = false;
    expect(fetcher.mock.calls[0]![2].aborted).toBe(true);
    pending.resolve(page("private"));
    await Promise.resolve();
    expect(search.items.value).toEqual([]);
    expect(search.status.value).toBe("idle");
  });

  it("preserves the loaded page on pagination failure and retries the same opaque cursor", async () => {
    const fetcher = vi
      .fn<Parameters<typeof useGameSearch>[2]>()
      .mockResolvedValueOnce(page("first", "opaque"))
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({
        items: [...page("first").items, ...page("second").items],
        nextCursor: null,
      });
    const { query, search } = setup(fetcher);
    query.value = "Hollow";
    await vi.advanceTimersByTimeAsync(300);
    await search.loadMore();
    expect(search.moreError.value).toBe(true);
    expect(search.items.value[0]?.game.id).toBe("first");
    expect(search.nextCursor.value).toBe("opaque");
    await search.loadMore();
    expect(fetcher.mock.calls[2]?.slice(0, 2)).toEqual(["Hollow", "opaque"]);
    expect(search.items.value.map((hit) => hit.game.id)).toEqual(["first", "second"]);
    expect(search.moreError.value).toBe(false);
    expect(search.nextCursor.value).toBeNull();
  });

  it("does not append an old page to a changed search", async () => {
    const more = deferred<CatalogPage>();
    const fetcher = vi
      .fn<Parameters<typeof useGameSearch>[2]>()
      .mockResolvedValueOnce(page("first", "old-cursor"))
      .mockReturnValueOnce(more.promise)
      .mockResolvedValueOnce(page("different"));
    const { query, search } = setup(fetcher);
    query.value = "Hollow";
    await vi.advanceTimersByTimeAsync(300);
    const loading = search.loadMore();
    query.value = "Zelda";
    await vi.advanceTimersByTimeAsync(300);
    more.resolve(page("old-page"));
    await loading;
    expect(search.items.value.map((hit) => hit.game.id)).toEqual(["different"]);
    expect(fetcher.mock.calls[2]?.[1]).toBeNull();
  });

  it("offers retry after failure and distinguishes a successful empty result", async () => {
    const fetcher = vi
      .fn<Parameters<typeof useGameSearch>[2]>()
      .mockRejectedValueOnce(new Error("unavailable"))
      .mockResolvedValueOnce({ items: [], nextCursor: null });
    const { query, search } = setup(fetcher);
    query.value = "Unknown";
    await vi.advanceTimersByTimeAsync(300);
    expect(search.status.value).toBe("error");
    await search.retry();
    expect(search.status.value).toBe("success");
    expect(search.items.value).toEqual([]);
  });

  it("cancels scheduled and in-flight work when leaving the page", async () => {
    const fetcher = vi
      .fn<Parameters<typeof useGameSearch>[2]>()
      .mockReturnValue(new Promise(() => {}));
    const first = setup(fetcher);
    first.query.value = "Hollow";
    first.scope.stop();
    await vi.advanceTimersByTimeAsync(300);
    expect(fetcher).not.toHaveBeenCalled();
    const second = setup(fetcher);
    second.query.value = "Hades";
    await vi.advanceTimersByTimeAsync(300);
    second.scope.stop();
    expect(fetcher.mock.calls[0]![2].aborted).toBe(true);
  });
});

it("loads a committed search immediately while a separate draft only drives delayed suggestions", async () => {
  const scope = effectScope();
  scopes.push(scope);
  const committed = ref("");
  const draft = ref("");
  const enabled = ref(true);
  const fetchResults = vi
    .fn<Parameters<typeof useGameSearch>[2]>()
    .mockResolvedValue(page("result"));
  const fetchSuggestions = vi
    .fn<Parameters<typeof useGameSearch>[2]>()
    .mockResolvedValue(page("suggestion"));
  scope.run(() => {
    useGameSearch(committed, enabled, fetchResults, 0);
    useGameSearch(draft, enabled, fetchSuggestions, 500);
  });
  draft.value = "Hollow";
  await vi.advanceTimersByTimeAsync(499);
  expect(fetchResults).not.toHaveBeenCalled();
  expect(fetchSuggestions).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(1);
  expect(fetchSuggestions).toHaveBeenCalledTimes(1);
  expect(fetchResults).not.toHaveBeenCalled();
  committed.value = draft.value;
  expect(fetchResults).toHaveBeenCalledWith("Hollow", null, expect.any(AbortSignal));
  draft.value = "Hollow Knight";
  await vi.advanceTimersByTimeAsync(500);
  expect(fetchResults).toHaveBeenCalledTimes(1);
  expect(fetchSuggestions).toHaveBeenCalledTimes(2);
});
