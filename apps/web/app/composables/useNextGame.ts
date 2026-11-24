import { computed, onScopeDispose, ref, watch, type Ref } from "vue";
import type { LibraryEntryView, NextGameView } from "#shared/library";

export interface NextGameTransport {
  draw: (
    query: { platformId?: string; excludeGameId?: string },
    signal: AbortSignal,
  ) => Promise<NextGameView>;
  /** Sets the game's status to `playing`; only called after an explicit user action. */
  start: (gameId: string) => Promise<unknown>;
}

/**
 * Draws random games from the own backlog. Drawing never changes any state; only {@link start}
 * sets the drawn game to `playing`. A redraw excludes the current game, so returning the same game
 * again means it is the only candidate.
 *
 * @param platformId - Current platform filter; a change discards the current draw.
 * @param transport - {@link NextGameTransport} for drawing and starting.
 * @returns Reactive draw state plus `draw` and `start`.
 */
export function useNextGame(platformId: Ref<string | undefined>, transport: NextGameTransport) {
  /** `undefined` before the first draw, `null` when no backlog game matches. */
  const entry = ref<LibraryEntryView | null>();
  const drawing = ref(false);
  const failed = ref(false);
  const onlyCandidate = ref(false);
  const starting = ref(false);
  /** Drawn games are always `backlog`; only a successful {@link start} sets `playing`. */
  const started = computed(() => entry.value?.status === "playing");
  const startError = ref("");
  let controller: AbortController | undefined;
  let generation = 0;

  function cancel() {
    generation++;
    controller?.abort();
  }
  watch(
    platformId,
    () => {
      cancel();
      entry.value = undefined;
      drawing.value = false;
      failed.value = false;
      onlyCandidate.value = false;
      startError.value = "";
    },
    { flush: "sync" },
  );
  onScopeDispose(cancel);

  async function draw() {
    if (starting.value) {
      return;
    }
    controller?.abort();
    const current = ++generation;
    controller = new AbortController();
    // A started game is no longer in the backlog and needs no exclusion.
    const excludeGameId = !started.value ? entry.value?.gameId : undefined;
    drawing.value = true;
    failed.value = false;
    startError.value = "";
    try {
      const result = await transport.draw(
        {
          ...(platformId.value ? { platformId: platformId.value } : {}),
          ...(excludeGameId ? { excludeGameId } : {}),
        },
        controller.signal,
      );
      if (current !== generation) {
        return;
      }
      onlyCandidate.value = !!excludeGameId && result.entry?.gameId === excludeGameId;
      entry.value = result.entry;
    } catch {
      if (current === generation) {
        failed.value = true;
      }
    } finally {
      if (current === generation) {
        drawing.value = false;
      }
    }
  }

  async function start() {
    const target = entry.value;
    if (!target || starting.value || started.value || drawing.value) {
      return;
    }
    const current = generation;
    starting.value = true;
    startError.value = "";
    try {
      await transport.start(target.gameId);
      if (current === generation) {
        entry.value = { ...target, status: "playing" };
        onlyCandidate.value = false;
      }
    } catch {
      if (current === generation) {
        startError.value = "Der Status konnte nicht gespeichert werden. Bitte versuche es erneut.";
      }
    } finally {
      starting.value = false;
    }
  }

  return { entry, drawing, failed, onlyCandidate, starting, started, startError, draw, start };
}
