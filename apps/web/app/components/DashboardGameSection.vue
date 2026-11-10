<script setup lang="ts">
import type { LibraryPageView, PlayStatus } from "#shared/library";
defineProps<{
  title: string;
  subtitle?: string;
  page: LibraryPageView | null | undefined;
  pending: boolean;
  failed: boolean;
  status: PlayStatus;
  compact?: boolean;
}>();
const emit = defineEmits<{ retry: [] }>();
function platform(entry: LibraryPageView["items"][number]) {
  return entry.platformId
    ? (entry.game?.platforms.find((item) => item.id === entry.platformId)?.name ??
        "Plattform nicht verfügbar")
    : "Ohne Plattform";
}
</script>

<template>
  <section class="dashboard-panel" :aria-labelledby="`section-${status}`">
    <div class="section-heading">
      <div>
        <h2 :id="`section-${status}`">{{ title }}</h2>
        <p v-if="subtitle">{{ subtitle }}</p>
      </div>
      <NuxtLink :to="{ path: '/library', query: { status } }"
        >{{ compact ? "Alle aktiven Spiele" : "Backlog öffnen" }}
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.7"
          aria-hidden="true"
        >
          <path d="M4 12h15m-6-6 6 6-6 6" /></svg
      ></NuxtLink>
    </div>
    <output v-if="pending" class="panel-state">Spiele werden geladen …</output>
    <div v-else-if="failed" class="panel-state" role="alert">
      <p>Diese Spiele konnten nicht geladen werden.</p>
      <button type="button" class="secondary" @click="emit('retry')">Erneut versuchen</button>
    </div>
    <template v-else-if="page">
      <p v-if="page.catalogUnavailable" class="catalog-note">
        Katalog nicht erreichbar. Gespeicherte Spieldaten werden angezeigt.
      </p>
      <div v-if="!page.items.length" class="panel-state">
        <h3>{{ compact ? "Bereit für die nächste Runde?" : "Platz für neue Entdeckungen" }}</h3>
        <p>
          {{
            compact
              ? "Wähle ein Spiel aus deinem Backlog und setze seinen Status auf „Spiele ich gerade“."
              : "Füge ein Spiel hinzu, das du als Nächstes spielen möchtest."
          }}
        </p>
        <NuxtLink
          :to="compact ? { path: '/library', query: { status: 'backlog' } } : '/'"
          class="button secondary"
          >{{ compact ? "Zum Backlog" : "Spiele entdecken" }}</NuxtLink
        >
      </div>
      <ul v-else :class="compact ? 'playing-list' : 'backlog-grid'">
        <li v-for="entry in page.items" :key="entry.gameId">
          <NuxtLink
            :to="{ path: `/games/${entry.gameId}`, query: { returnTo: '/dashboard' } }"
            :class="compact ? 'playing-row' : 'game-link'"
            ><GameCover :src="entry.display.coverUrl" :title="entry.display.title" />
            <div class="playing-info">
              <h3>{{ entry.display.title }}</h3>
              <p>{{ platform(entry) }}</p>
            </div>
            <template v-if="compact"
              ><span class="play-status"><span aria-hidden="true" /> Gerade aktiv</span
              ><span class="playing-open"
                >Spieldetails
                <svg
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.7"
                  aria-hidden="true"
                >
                  <path d="M4 12h15m-6-6 6 6-6 6" /></svg></span></template
          ></NuxtLink>
        </li>
      </ul>
    </template>
  </section>
</template>
