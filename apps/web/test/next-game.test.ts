import { afterEach, expect, it, vi } from "vite-plus/test";
import { effectScope, nextTick, ref } from "vue";
import { randomUUID } from "node:crypto";
import { useNextGame, type NextGameTransport } from "../app/composables/useNextGame";
import type { LibraryEntryView, NextGameView } from "../shared/library";

const scopes: ReturnType<typeof effectScope>[] = [];
afterEach(() => scopes.splice(0).forEach((scope) => scope.stop()));
function entry(title = "Hades"): LibraryEntryView {
  return {
    gameId: randomUUID(),
    platformId: null,
    status: "backlog",
    favorite: false,
    rating: null,
    note: null,
    ownership: "owned",
    game: null,
    display: { title, coverUrl: null },
    catalogStatus: "available",
  };
}
function drawn(value: LibraryEntryView | null, catalogUnavailable = false): NextGameView {
  return { entry: value, catalogUnavailable };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}
function setup(platform?: string) {
  const transport = {
    draw: vi.fn<NextGameTransport["draw"]>(),
    start: vi.fn<NextGameTransport["start"]>().mockResolvedValue({}),
  };
  const platformId = ref<string | undefined>(platform);
  const scope = effectScope();
  scopes.push(scope);
  const flow = scope.run(() => useNextGame(platformId, transport))!;
  return { transport, platformId, flow };
}

it("draws without changing state and excludes the previous game on a redraw", async () => {
  const { flow, transport } = setup("20000000-0000-4000-8000-000000000001");
  const first = entry("Hades");
  const second = entry("Celeste");
  transport.draw.mockResolvedValueOnce(drawn(first)).mockResolvedValueOnce(drawn(second));
  expect(transport.draw).not.toHaveBeenCalled();
  await flow.draw();
  expect(transport.draw.mock.calls[0]![0]).toEqual({
    platformId: "20000000-0000-4000-8000-000000000001",
  });
  expect(flow.entry.value).toEqual(first);
  expect(flow.reveal.value).toBe(1);
  await flow.draw();
  expect(transport.draw.mock.calls[1]![0]).toEqual({
    platformId: "20000000-0000-4000-8000-000000000001",
    excludeGameId: first.gameId,
  });
  expect(flow.entry.value).toEqual(second);
  expect(flow.onlyCandidate.value).toBe(false);
  expect(flow.reveal.value).toBe(2);
  expect(transport.start).not.toHaveBeenCalled();
});

it("reports a single candidate when the redraw returns the excluded game", async () => {
  const { flow, transport } = setup();
  const only = entry();
  transport.draw.mockResolvedValue(drawn(only));
  await flow.draw();
  expect(flow.onlyCandidate.value).toBe(false);
  await flow.draw();
  expect(transport.draw.mock.calls[1]![0]).toEqual({ excludeGameId: only.gameId });
  expect(flow.onlyCandidate.value).toBe(true);
  expect(flow.entry.value?.gameId).toBe(only.gameId);
});

it("distinguishes an empty backlog from a pending first draw", async () => {
  const { flow, transport } = setup();
  transport.draw.mockResolvedValue(drawn(null));
  expect(flow.drawn.value).toBe(false);
  await flow.draw();
  expect(flow.drawn.value).toBe(true);
  expect(flow.entry.value).toBeNull();
});

it("keeps the catalog outage flag with the drawn snapshot", async () => {
  const { flow, transport } = setup();
  transport.draw.mockResolvedValue(drawn({ ...entry(), catalogStatus: "unavailable" }, true));
  await flow.draw();
  expect(flow.catalogUnavailable.value).toBe(true);
  expect(flow.entry.value?.display.title).toBe("Hades");
});

it("starts only explicitly and does not exclude a started game on the next draw", async () => {
  const { flow, transport } = setup();
  const first = entry();
  transport.draw.mockResolvedValueOnce(drawn(first)).mockResolvedValueOnce(drawn(null));
  await flow.draw();
  expect(await flow.start()).toBe(true);
  expect(transport.start).toHaveBeenCalledExactlyOnceWith(first.gameId);
  expect(flow.started.value).toBe(true);
  expect(flow.entry.value?.status).toBe("playing");
  expect(await flow.start()).toBe(false);
  expect(transport.start).toHaveBeenCalledOnce();
  await flow.draw();
  expect(transport.draw.mock.calls[1]![0]).toEqual({});
  expect(flow.started.value).toBe(false);
});

it("keeps the drawn game after a failed start and allows a retry", async () => {
  const { flow, transport } = setup();
  transport.draw.mockResolvedValue(drawn(entry()));
  transport.start.mockRejectedValueOnce(new Error("offline"));
  await flow.draw();
  expect(await flow.start()).toBe(false);
  expect(flow.startError.value).not.toBe("");
  expect(flow.entry.value?.status).toBe("backlog");
  expect(await flow.start()).toBe(true);
  expect(flow.startError.value).toBe("");
});

it("ignores stale draws, keeps the last game on failure and resets on a platform change", async () => {
  const { flow, transport, platformId } = setup();
  const kept = entry("Kept");
  transport.draw.mockResolvedValueOnce(drawn(kept));
  await flow.draw();
  const slow = deferred<NextGameView>();
  transport.draw.mockReturnValueOnce(slow.promise).mockRejectedValueOnce(new Error("503"));
  const stale = flow.draw();
  const signal = transport.draw.mock.calls[1]![1];
  await flow.draw();
  expect(signal.aborted).toBe(true);
  slow.resolve(drawn(entry("Stale")));
  await stale;
  expect(flow.failed.value).toBe(true);
  expect(flow.drawing.value).toBe(false);
  expect(flow.entry.value).toEqual(kept);
  platformId.value = "20000000-0000-4000-8000-000000000001";
  await nextTick();
  expect(flow.entry.value).toBeNull();
  expect(flow.drawn.value).toBe(false);
  expect(flow.failed.value).toBe(false);
});
