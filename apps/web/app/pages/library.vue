<script setup lang="ts">
import {
  libraryFilters,
  statusLabels,
  type LibraryPageView,
  type LibraryEntryView,
} from "#shared/library";
definePageMeta({ middleware: "auth" });
useHead({ title: "Meine Bibliothek · Unplayd" });
const route = useRoute();
const router = useRouter();
const requestFetch = useRequestFetch();
const filters = computed(() => libraryFilters(route.query));
// Start interactive list state after hydration; SSR and the first client render share the loading state.
const enabled = ref(false);
onMounted(() => {
  enabled.value = true;
});
const { page, loading, refreshing, failed, pending, message, mutationError, refresh, mutate } =
  useLibrary(
    filters,
    {
      list: (query, signal) =>
        requestFetch<LibraryPageView>("/api/library", { query: { ...query, limit: 24 }, signal }),
      status: (id, status) =>
        $fetch(`/api/library/${id}/status`, { method: "PUT", body: { status } }),
      favorite: (id, favorite) =>
        $fetch(`/api/favorites/${id}`, { method: favorite ? "PUT" : "DELETE" }),
      remove: (id) => $fetch<null>(`/api/library/${id}`, { method: "DELETE" }),
    },
    enabled,
  );
const {
  data: platforms,
  error: platformsError,
  refresh: refreshPlatforms,
} = await useLazyFetch("/api/library/platforms");
const knownEntries = useKnownEntries();
const removeTarget = ref<LibraryEntryView | null>(null);
const heading = ref<HTMLHeadingElement | null>(null);
async function remove(id: string) {
  removeTarget.value = null;
  if (await mutate(id)) {
    knownEntries.value[id] = false;
    void refreshPlatforms();
  }
  await nextTick();
  heading.value?.focus({ preventScroll: true });
}
function freezeLeavingCard(element: Element) {
  const card = element as HTMLElement;
  const { width, height } = card.getBoundingClientRect();
  // Keep the leaving card in place while the keyed survivors move into its slot.
  Object.assign(card.style, {
    width: `${width}px`,
    height: `${height}px`,
    left: `${card.offsetLeft}px`,
    top: `${card.offsetTop}px`,
  });
}
function clearLeavingCard(element: Element) {
  const card = element as HTMLElement;
  for (const property of ["width", "height", "left", "top"]) {
    card.style.removeProperty(property);
  }
}
const filtered = computed(
  () => !!(filters.value.status || filters.value.platformId || filters.value.favorite),
);
function filter(key: "status" | "platformId" | "favorite", value: string) {
  return router.push({
    path: "/library",
    query: { ...filters.value, [key]: value || undefined, cursor: undefined },
  });
}
</script>

<template>
  <section>
    <div class="page-intro library-heading">
      <div>
        <h1 ref="heading" tabindex="-1">Meine Bibliothek</h1>
        <p>Deine Spiele. Dein Tempo.</p>
      </div>
      <NuxtLink to="/" class="button secondary">Spiel hinzufügen</NuxtLink>
    </div>
    <div class="library-filters">
      <div>
        <label for="library-status">Status</label
        ><select
          id="library-status"
          :value="filters.status ?? ''"
          @change="filter('status', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">Alle Status</option>
          <option v-for="(label, value) in statusLabels" :key="value" :value="value">
            {{ label }}
          </option>
        </select>
      </div>
      <div>
        <label for="library-platform">Plattform</label
        ><select
          id="library-platform"
          :value="filters.platformId ?? ''"
          @change="filter('platformId', ($event.target as HTMLSelectElement).value)"
        >
          <option value="">Alle Plattformen</option>
          <option
            v-if="filters.platformId && !platforms?.some((p) => p.id === filters.platformId)"
            :value="filters.platformId"
          >
            Ausgewählte Plattform
          </option>
          <option v-for="platform in platforms" :key="platform.id" :value="platform.id">
            {{ platform.name }}
          </option>
        </select>
      </div>
      <label class="favorite-filter"
        ><input
          type="checkbox"
          :checked="!!filters.favorite"
          @change="filter('favorite', ($event.target as HTMLInputElement).checked ? 'true' : '')"
        />
        Nur Favoriten</label
      >
      <NuxtLink v-if="filtered || filters.cursor" to="/library">Filter zurücksetzen</NuxtLink>
    </div>
    <div v-if="platformsError" role="alert">
      <p>Plattformauswahl konnte nicht geladen werden.</p>
      <button type="button" class="secondary" @click="refreshPlatforms()">
        Plattformen erneut laden
      </button>
    </div>
    <output aria-live="polite" class="library-feedback">{{ message }}</output>
    <p v-if="mutationError" role="alert" class="error-message">{{ mutationError }}</p>
    <output v-if="loading" class="state-message">Bibliothek wird geladen …</output>
    <div v-if="failed" role="alert" class="state-message">
      <p>Die Bibliothek konnte nicht geladen werden.</p>
      <button type="button" @click="refresh()">Erneut versuchen</button>
    </div>
    <template v-if="page">
      <p v-if="page.catalogUnavailable" class="notice">
        Der Katalog ist gerade nicht erreichbar. Deine gespeicherten Spieldaten und Status bleiben
        verfügbar.
      </p>
      <div v-if="!page.items.length && !pending" class="state-message">
        <h2>
          {{
            filtered || filters.cursor
              ? "Keine Spiele auf dieser Seite"
              : "Deine Bibliothek wartet auf das erste Spiel"
          }}
        </h2>
        <p>
          {{
            filtered || filters.cursor
              ? "Ändere die Filter oder kehre zur ersten Seite zurück."
              : "Entdecke ein Spiel und füge es deiner Bibliothek hinzu."
          }}
        </p>
        <NuxtLink to="/" class="button">Spiele entdecken</NuxtLink>
      </div>
      <TransitionGroup
        tag="ul"
        name="library-card"
        class="game-grid library-grid"
        :aria-busy="!!pending || refreshing"
        @before-leave="freezeLeavingCard"
        @after-leave="clearLeavingCard"
        @leave-cancelled="clearLeavingCard"
      >
        <li v-for="entry in page.items" :key="entry.gameId">
          <LibraryEntryCard
            :entry="entry"
            :return-to="route.fullPath"
            :disabled="!!pending || refreshing"
            :saving="pending === entry.gameId"
            @status="mutate(entry.gameId, { status: $event })"
            @favorite="mutate(entry.gameId, { favorite: !entry.favorite })"
            @remove="removeTarget = entry"
          />
        </li>
      </TransitionGroup>
      <nav class="pagination" aria-label="Bibliotheksseiten">
        <NuxtLink
          v-if="filters.cursor"
          :to="{ path: '/library', query: { ...filters, cursor: undefined } }"
          >Zur ersten Seite</NuxtLink
        ><NuxtLink
          v-if="page.nextCursor"
          :to="{ path: '/library', query: { ...filters, cursor: page.nextCursor } }"
          >Nächste Seite</NuxtLink
        >
      </nav>
    </template>
    <RemoveGameDialog
      :entry="removeTarget"
      @cancel="removeTarget = null"
      @confirm="remove($event)"
    />
  </section>
</template>
