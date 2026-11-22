import { onScopeDispose, ref, watch, type Ref } from "vue";
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
  const entry = ref<LibraryEntryView | null>(null);
  /** `true` once a draw has completed, so `entry === null` means an empty backlog. */
  const drawn = ref(false);
  const drawing = ref(false);
  const failed = ref(false);
  const onlyCandidate = ref(false);
  /** Increments for every newly revealed game to restart the reveal animation. */
  const reveal = ref(0);
  const starting = ref(false);
  const started = ref(false);
  const startError = ref("");
  let controller: AbortController | undefined;
  let generation = 0;
  let disposed = false;

  function clear() {
    generation++;
    controller?.abort();
    entry.value = null;
    drawn.value = false;
    drawing.value = false;
    failed.value = false;
    onlyCandidate.value = false;
    started.value = false;
    startError.value = "";
  }
  watch(platformId, clear, { flush: "sync" });
  onScopeDispose(() => {
    disposed = true;
    generation++;
    controller?.abort();
  });

  async function draw() {
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
      if (current !== generation || disposed) {
        return;
      }
      onlyCandidate.value = !!excludeGameId && result.entry?.gameId === excludeGameId;
      entry.value = result.entry;
      started.value = false;
      drawn.value = true;
      reveal.value++;
    } catch {
      if (current === generation && !disposed) {
        failed.value = true;
      }
    } finally {
      if (current === generation && !disposed) {
        drawing.value = false;
      }
    }
  }

  async function start() {
    const target = entry.value;
    if (!target || starting.value || started.value || drawing.value) {
      return false;
    }
    const current = generation;
    starting.value = true;
    startError.value = "";
    try {
      await transport.start(target.gameId);
      if (current === generation && !disposed) {
        entry.value = { ...target, status: "playing" };
        started.value = true;
        onlyCandidate.value = false;
      }
      return true;
    } catch {
      if (current === generation && !disposed) {
        startError.value = "Der Status konnte nicht gespeichert werden. Bitte versuche es erneut.";
      }
      return false;
    } finally {
      starting.value = false;
    }
  }

  return {
    entry,
    drawn,
    drawing,
    failed,
    onlyCandidate,
    reveal,
    starting,
    started,
    startError,
    draw,
    start,
  };
}
