import { afterEach, expect, it, vi } from "vite-plus/test";
import { effectScope, ref } from "vue";
import { randomUUID } from "node:crypto";
import { useLibrary, type LibraryTransport } from "../app/composables/useLibrary";
import {
  libraryFilters,
  libraryReturnPath,
  type LibraryFilters,
  type LibraryPageView,
} from "../shared/library";

const scopes: ReturnType<typeof effectScope>[] = [];
afterEach(() => scopes.splice(0).forEach((scope) => scope.stop()));
function fixture(): LibraryPageView {
  return {
    items: [
      {
        gameId: randomUUID(),
        platformId: null,
        status: "backlog",
        ownership: "owned",
        game: null,
        display: { title: "Hades", coverUrl: null },
        catalogStatus: "unavailable",
      },
    ],
    nextCursor: "next",
    catalogUnavailable: true,
  };
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
async function setup() {
  const initial = fixture();
  const transport = {
    list: vi.fn<LibraryTransport["list"]>().mockResolvedValue(initial),
    status: vi.fn<LibraryTransport["status"]>().mockResolvedValue({}),
    remove: vi.fn<LibraryTransport["remove"]>().mockResolvedValue(undefined),
  };
  const filters = ref<LibraryFilters>({});
  const scope = effectScope();
  scopes.push(scope);
  const flow = scope.run(() => useLibrary(filters, transport))!;
  await Promise.resolve();
  return { initial, transport, filters, flow, scope, id: initial.items[0]!.gameId };
}

it("rolls back optimistic status and removal failures and allows retry", async () => {
  const { flow, transport, id } = await setup();
  const status = deferred<unknown>();
  transport.status.mockReturnValueOnce(status.promise);
  const save = flow.mutate(id, "playing");
  expect(flow.page.value?.items[0]?.status).toBe("playing");
  await flow.mutate(id, "completed");
  expect(transport.status).toHaveBeenCalledTimes(1);
  status.reject(new Error("offline"));
  await save;
  expect(flow.page.value?.items[0]?.status).toBe("backlog");
  expect(flow.mutationError.value).toBeTruthy();
  const removal = deferred<unknown>();
  transport.remove.mockReturnValueOnce(removal.promise);
  const remove = flow.mutate(id);
  expect(flow.page.value?.items).toEqual([]);
  removal.reject(new Error("offline"));
  await remove;
  expect(flow.page.value?.items[0]?.gameId).toBe(id);
  expect(await flow.mutate(id)).toBe(true);
  expect(flow.message.value).toContain("entfernt");
});

it("keeps a confirmed mutation when the subsequent page refresh fails", async () => {
  const { flow, transport, id } = await setup();
  transport.list.mockRejectedValueOnce(new Error("offline"));
  expect(await flow.mutate(id, "playing")).toBe(true);
  expect(flow.page.value?.items[0]?.status).toBe("playing");
  expect(flow.message.value).toContain("gespeichert");
  expect(flow.mutationError.value).toBe("");
  expect(flow.failed.value).toBe(true);
});

it("keeps the remaining cards available throughout a delayed refresh after deletion", async () => {
  const { flow, transport, id, initial } = await setup();
  const survivor = fixture().items[0]!;
  flow.page.value = { ...initial, items: [...initial.items, survivor] };
  const updatedPage = deferred<LibraryPageView>();
  transport.list.mockReturnValueOnce(updatedPage.promise);
  const remove = flow.mutate(id);
  await Promise.resolve();
  expect(flow.page.value?.items).toEqual([survivor]);
  expect(flow.loading.value).toBe(false);
  expect(flow.refreshing.value).toBe(true);
  expect(flow.pending.value).toBe(id);
  updatedPage.resolve({ ...initial, items: [survivor] });
  await remove;
  expect(flow.refreshing.value).toBe(false);
  expect(flow.pending.value).toBeNull();
  expect(flow.page.value?.items).toEqual([survivor]);
});

it("keeps the list mounted while reconciling an optimistic status update", async () => {
  const { flow, transport, id, initial } = await setup();
  const response = deferred<LibraryPageView>();
  transport.list.mockReturnValueOnce(response.promise);
  const save = flow.mutate(id, "playing");
  await Promise.resolve();
  expect(flow.loading.value).toBe(false);
  expect(flow.refreshing.value).toBe(true);
  expect(flow.pending.value).toBe(id);
  expect(flow.page.value?.items[0]?.status).toBe("playing");
  response.resolve({ ...initial, items: [{ ...initial.items[0]!, status: "playing" }] });
  await save;
  expect(flow.pending.value).toBeNull();
  expect(flow.refreshing.value).toBe(false);
});

it("replaces the previous status confirmation without clearing it during the next write", async () => {
  const { flow, transport, id } = await setup();
  await flow.mutate(id, "playing");
  const previousMessage = flow.message.value;
  const update = deferred<unknown>();
  transport.status.mockReturnValueOnce(update.promise);
  const save = flow.mutate(id, "completed");
  expect(flow.message.value).toBe(previousMessage);
  update.resolve(undefined);
  await save;
  expect(flow.message.value).toContain("Abgeschlossen gespeichert");
});

it("passes status, platform and opaque cursor and discards late results after a filter change", async () => {
  const { flow, transport, filters, initial } = await setup();
  const old = deferred<LibraryPageView>();
  transport.list.mockReturnValueOnce(old.promise);
  filters.value = { status: "playing", platformId: randomUUID(), cursor: "opaque+/=" };
  const signal = transport.list.mock.calls[1]![1];
  expect(transport.list.mock.calls[1]![0]).toEqual(filters.value);
  const latest = fixture();
  transport.list.mockResolvedValueOnce(latest);
  filters.value = { status: "completed" };
  expect(signal.aborted).toBe(true);
  await Promise.resolve();
  old.resolve(initial);
  await Promise.resolve();
  expect(flow.page.value).toEqual(latest);
});

it("does not restore an obsolete page when a pending mutation fails after navigation", async () => {
  const { flow, transport, filters, id } = await setup();
  const pending = deferred<unknown>();
  transport.status.mockReturnValueOnce(pending.promise);
  const save = flow.mutate(id, "playing");
  const latest = fixture();
  transport.list.mockResolvedValueOnce(latest);
  filters.value = { status: "completed" };
  await Promise.resolve();
  pending.reject(new Error("offline"));
  await save;
  expect(flow.page.value).toEqual(latest);
  expect(flow.mutationError.value).toBe("");
});

it.each(["status", "removal"] as const)(
  "reconciles a successful %s with the current filters after an earlier filter fetch completes",
  async (operation) => {
    const { flow, transport, filters, initial, id } = await setup();
    const write = deferred<unknown>();
    if (operation === "status") {
      transport.status.mockReturnValueOnce(write.promise);
    } else {
      transport.remove.mockReturnValueOnce(write.promise);
    }
    const save = flow.mutate(id, operation === "status" ? "playing" : undefined);
    const beforeCommit: LibraryPageView = {
      ...initial,
      items: operation === "status" ? [] : initial.items,
    };
    transport.list.mockResolvedValueOnce(beforeCommit);
    filters.value = operation === "status" ? { status: "playing" } : { platformId: randomUUID() };
    await Promise.resolve();
    expect(flow.page.value).toEqual(beforeCommit);
    const afterCommit: LibraryPageView = {
      ...initial,
      items: operation === "status" ? [{ ...initial.items[0]!, status: "playing" }] : [],
    };
    transport.list.mockResolvedValueOnce(afterCommit);
    write.resolve(undefined);
    expect(await save).toBe(true);
    expect(transport.list).toHaveBeenCalledTimes(3);
    expect(transport.list.mock.calls.at(-1)![0]).toEqual(filters.value);
    expect(flow.page.value).toEqual(afterCommit);
    expect(flow.message.value).toBe("");
    expect(flow.mutationError.value).toBe("");
    expect(flow.pending.value).toBeNull();
  },
);

it("does not refresh after a pending write succeeds following unmount", async () => {
  const { flow, transport, scope, id } = await setup();
  const write = deferred<unknown>();
  transport.status.mockReturnValueOnce(write.promise);
  const save = flow.mutate(id, "playing");
  scope.stop();
  write.resolve(undefined);
  expect(await save).toBe(true);
  expect(transport.list).toHaveBeenCalledTimes(1);
  expect(flow.message.value).toBe("");
});

it("distinguishes load errors and empty results, and aborts on unmount", async () => {
  const { flow, transport, scope } = await setup();
  transport.list.mockRejectedValueOnce(new Error("offline"));
  await flow.refresh();
  expect(flow.failed.value).toBe(true);
  transport.list.mockResolvedValueOnce({ items: [], nextCursor: null, catalogUnavailable: false });
  await flow.refresh();
  expect(flow.failed.value).toBe(false);
  expect(flow.page.value?.items).toEqual([]);
  transport.list.mockReturnValueOnce(new Promise(() => {}));
  void flow.refresh();
  scope.stop();
  expect(transport.list.mock.calls.at(-1)![1].aborted).toBe(true);
});

it("validates URL filters and permits only local library return destinations", () => {
  const platformId = randomUUID();
  expect(libraryFilters({ status: "playing", platformId, cursor: "opaque+/=" })).toEqual({
    status: "playing",
    platformId,
    cursor: "opaque+/=",
  });
  expect(libraryFilters({ status: "toString", platformId: "invalid", cursor: ["a", "b"] })).toEqual(
    {},
  );
  const path = `/library?status=playing&platformId=${platformId}&cursor=opaque%2B%2F%3D`;
  expect(libraryReturnPath(path)).toBe(path);
  expect(libraryReturnPath("/dashboard")).toBe("/dashboard");
  for (const value of [
    "//foreign.example",
    "https://foreign.example",
    "/library/../sign-in",
    [path],
    undefined,
  ]) {
    expect(libraryReturnPath(value)).toBeNull();
  }
});

it("waits for hydration before requesting the interactive list", async () => {
  const scope = effectScope();
  scopes.push(scope);
  const enabled = ref(false);
  const list = vi.fn<LibraryTransport["list"]>().mockResolvedValue(fixture());
  const flow = scope.run(() =>
    useLibrary(ref({}), { list, status: vi.fn(), remove: vi.fn() }, enabled),
  )!;
  expect(list).not.toHaveBeenCalled();
  expect(flow.loading.value).toBe(true);
  enabled.value = true;
  await Promise.resolve();
  expect(list).toHaveBeenCalledTimes(1);
  expect(flow.page.value?.items).toHaveLength(1);
});
