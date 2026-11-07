<script setup lang="ts">
import type { GameCardModel } from "../../shared/catalog";
const props = defineProps<{ query: string; enabled: boolean }>();
const emit = defineEmits<{
  search: [query: string];
  select: [game: GameCardModel, query: string];
}>();
const draft = ref(props.query);
const focused = ref(false);
const dismissed = ref(false);
const active = ref(-1);
const form = ref<HTMLFormElement>();
const list = ref<HTMLElement>();
const listId = "game-suggestions";
const suggestionEnabled = computed(
  () =>
    props.enabled &&
    focused.value &&
    !dismissed.value &&
    draft.value.trim().length >= 3 &&
    draft.value.trim() !== props.query.trim(),
);
const { items, status } = useGameSearch(
  draft,
  suggestionEnabled,
  (term, _cursor, signal) =>
    $fetch("/api/games/suggestions", { query: { query: term }, signal, retry: 0 }),
  500,
);
const matches = computed(() => items.value.slice(0, 5).map((hit) => hit.game));
const expanded = computed(
  () => suggestionEnabled.value && status.value === "success" && matches.value.length > 0,
);
const activeId = computed(() =>
  expanded.value && active.value >= 0 ? `${listId}-${active.value}` : undefined,
);
const valid = computed(() => draft.value.trim().length >= 2 && draft.value.trim().length <= 200);
watch(
  draft,
  () => {
    dismissed.value = false;
    active.value = -1;
  },
  { flush: "sync" },
);
watch(items, () => {
  active.value = -1;
});
watch(active, async (index) => {
  if (index < 0) return;
  await nextTick();
  list.value?.children.item(index)?.scrollIntoView({ block: "nearest" });
});
watch(
  () => props.query,
  (value) => {
    draft.value = value;
    dismissed.value = true;
    active.value = -1;
  },
);
function dismiss() {
  dismissed.value = true;
  active.value = -1;
}
function submit() {
  if (!valid.value) return;
  dismiss();
  emit("search", draft.value.trim());
}
function choose(game: GameCardModel) {
  dismiss();
  emit("select", game, draft.value.trim());
}
function move(direction: number) {
  dismissed.value = false;
  if (!expanded.value) return;
  if (active.value < 0) active.value = direction > 0 ? 0 : matches.value.length - 1;
  else active.value = (active.value + direction + matches.value.length) % matches.value.length;
}
function keydown(event: KeyboardEvent) {
  if (event.isComposing) return;
  if (event.key === "Escape") {
    event.preventDefault();
    dismiss();
    return;
  }
  if (event.key === "Tab") {
    dismiss();
    return;
  }
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    move(event.key === "ArrowDown" ? 1 : -1);
    return;
  }
  const selected = expanded.value ? matches.value[active.value] : undefined;
  if (event.key === "Enter" && selected) {
    event.preventDefault();
    choose(selected);
  }
}
function focusout(event: FocusEvent) {
  if (!form.value?.contains(event.relatedTarget as Node | null)) focused.value = false;
}
</script>

<template>
  <form
    ref="form"
    role="search"
    class="game-search-form"
    @submit.prevent="submit()"
    @focusout="focusout"
  >
    <label for="game-search">Spiele suchen</label>
    <div class="search-field">
      <input
        id="game-search"
        v-model="draft"
        type="search"
        role="combobox"
        aria-autocomplete="list"
        aria-haspopup="listbox"
        :aria-expanded="expanded"
        :aria-controls="expanded ? listId : undefined"
        :aria-activedescendant="activeId"
        name="query"
        placeholder="Spielnamen eingeben …"
        minlength="2"
        maxlength="200"
        autocomplete="off"
        aria-describedby="search-hint"
        @focus="
          focused = true;
          dismissed = false;
        "
        @keydown="keydown"
      />
      <button
        v-if="draft"
        type="button"
        aria-label="Suchfeld leeren"
        @click="
          draft = '';
          dismiss();
        "
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          aria-hidden="true"
        >
          <path d="m6 6 12 12M6 18 18 6" />
        </svg>
      </button>
      <button type="submit" class="search-submit" aria-label="Suche starten" :disabled="!valid">
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          aria-hidden="true"
        >
          <circle cx="10.5" cy="10.5" r="7.5" />
          <path d="m16 16 6 6" />
        </svg>
      </button>
    </div>
    <ul
      v-if="expanded"
      :id="listId"
      ref="list"
      role="listbox"
      aria-label="Spielvorschläge"
      class="search-suggestions"
    >
      <li
        v-for="(game, index) in matches"
        :id="`${listId}-${index}`"
        :key="game.id"
        role="option"
        :aria-selected="active === index"
        tabindex="-1"
        @mousedown.prevent
        @click="choose(game)"
        @keydown.enter.prevent="choose(game)"
        @pointermove="active = index"
      >
        <img
          v-if="game.coverUrl"
          :src="game.coverUrl"
          alt=""
          width="32"
          height="44"
          referrerpolicy="no-referrer"
        />
        <span v-else class="suggestion-cover" aria-hidden="true" />
        <span class="suggestion-text"
          ><strong>{{ game.title }}</strong
          ><span>{{
            game.platforms
              .slice(0, 2)
              .map((platform) => platform.name)
              .join(" · ")
          }}</span></span
        >
      </li>
    </ul>
    <p id="search-hint" class="muted">
      {{
        valid
          ? "Mit Enter oder der Lupe alle Treffer anzeigen."
          : "Gib mindestens zwei Zeichen ein."
      }}
    </p>
    <output v-if="suggestionEnabled && status === 'loading'" class="suggestion-hint"
      >Vorschläge werden geladen …</output
    >
    <output v-else-if="suggestionEnabled && status === 'error'" class="suggestion-hint"
      >Vorschläge gerade nicht verfügbar. Mit Enter suchen.</output
    >
    <output
      v-else-if="suggestionEnabled && status === 'success' && !matches.length"
      class="suggestion-hint"
      >Keine Vorschläge. Mit Enter alle Spiele durchsuchen.</output
    >
    <output v-if="expanded" class="visually-hidden"
      >{{ matches.length }} Spielvorschläge. Mit den Pfeiltasten auswählen und mit Enter
      öffnen.</output
    >
  </form>
</template>
