import * as vue from "vue";
import { afterEach, vi } from "vite-plus/test";
import { useGameSearch } from "../../app/composables/useGameSearch";

// The isolated component build has no Nuxt auto-import transform.
Object.assign(globalThis, {
  ref: vue.ref,
  computed: vue.computed,
  watch: vue.watch,
  nextTick: vue.nextTick,
  useGameSearch,
});
afterEach(() => vi.unstubAllGlobals());
