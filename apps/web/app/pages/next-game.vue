<script setup lang="ts">
import type { ComponentPublicInstance } from "vue";
import { libraryFilters, ownershipLabels, platformLabel, type NextGameView } from "#shared/library";
definePageMeta({ middleware: "auth" });
useHead({ title: "Nächstes Spiel · Unplayd" });
const route = useRoute();
const router = useRouter();
const platformId = computed(() => libraryFilters(route.query).platformId);
const { entry, drawing, failed, onlyCandidate, starting, started, startError, draw, start } =
  useNextGame(platformId, {
    draw: (query, signal) => $fetch<NextGameView>("/api/next-game", { query, signal }),
    start: (id) =>
      $fetch(`/api/library/${id}/status`, { method: "PUT", body: { status: "playing" } }),
  });
const {
  data: platforms,
  error: platformsError,
  refresh: refreshPlatforms,
} = useLazyFetch("/api/library/platforms");
const emptyState = computed(() =>
  platformId.value
    ? {
        title: "Kein Backlog-Spiel für diese Plattform",
        hint: "Wähle eine andere Plattform oder ziehe aus allen Plattformen.",
      }
    : {
        title: "Dein Backlog ist leer",
        hint: "Füge Spiele mit dem Status „Backlog“ hinzu, damit hier etwas auf dich wartet.",
      },
);
const onlyCandidateText = "Das ist das einzige passende Spiel in deinem Backlog.";
const startedText = "Viel Spaß! Das Spiel steht jetzt auf „Spiele ich gerade“.";
const announcement = computed(() => {
  if (drawing.value) {
    return "Spiel wird gezogen …";
  }
  // The error itself is announced by its alert; repeating the previous draw would contradict it.
  if (failed.value || entry.value === undefined) {
    return "";
  }
  if (!entry.value) {
    return `${emptyState.value.title}.`;
  }
  const title = `„${entry.value.display.title}“`;
  if (started.value) {
    return `${title}: ${startedText}`;
  }
  return onlyCandidate.value ? `${title}: ${onlyCandidateText}` : `${title} gezogen.`;
});
function selectPlatform(value: string) {
  return router.replace({ path: "/next-game", query: { platformId: value || undefined } });
}
// These actions remove the clicked button, so focus moves on instead of falling back to <body>.
const drawButton = useTemplateRef<HTMLButtonElement>("drawButton");
const startButton = useTemplateRef<HTMLButtonElement>("startButton");
const dashboardLink = useTemplateRef<ComponentPublicInstance>("dashboardLink");
function retry() {
  drawButton.value?.focus();
  return draw();
}
function showAllPlatforms() {
  drawButton.value?.focus();
  return selectPlatform("");
}
async function startGame() {
  await start();
  await nextTick();
  if (!document.activeElement || document.activeElement === document.body) {
    (started.value
      ? (dashboardLink.value?.$el as HTMLElement | undefined)
      : startButton.value
    )?.focus();
  }
}
</script>

<template>
  <section class="next-game-view">
    <div class="page-intro next-game-heading">
      <h1>Was spielst du als Nächstes?</h1>
      <p>
        Lass den Zufall aus deinem Backlog wählen. Dein Status ändert sich erst, wenn du startest.
      </p>
    </div>
    <div class="next-game-controls">
      <div>
        <label for="next-game-platform">Plattform</label
        ><select
          id="next-game-platform"
          :value="platformId ?? ''"
          :disabled="starting"
          @change="selectPlatform(($event.target as HTMLSelectElement).value)"
        >
          <option value="">Alle Plattformen</option>
          <option
            v-if="platformId && !platforms?.some((p) => p.id === platformId)"
            :value="platformId"
          >
            Ausgewählte Plattform
          </option>
          <option v-for="platform in platforms" :key="platform.id" :value="platform.id">
            {{ platform.name }}
          </option>
        </select>
      </div>
      <button
        ref="drawButton"
        type="button"
        class="draw-button"
        :disabled="starting"
        @click="draw()"
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.7"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <rect x="4" y="4" width="16" height="16" rx="3" />
          <path d="M9 9h.01M15 9h.01M9 15h.01M15 15h.01" stroke-width="2.5" />
        </svg>
        {{ entry ? "Neu ziehen" : "Spiel ziehen" }}
      </button>
    </div>
    <div v-if="platformsError" role="alert" class="notice">
      Plattformauswahl konnte nicht geladen werden.
      <button type="button" class="text-button" @click="refreshPlatforms()">Erneut laden</button>
    </div>

    <output class="visually-hidden" aria-live="polite">{{ announcement }}</output>
    <div class="next-game-stage">
      <div
        v-if="entry === undefined || (drawing && !entry)"
        class="card-stack"
        :class="{ shuffling: drawing }"
        aria-hidden="true"
      >
        <span /><span /><span />
      </div>

      <div v-if="failed" role="alert" class="state-message">
        <p>Es konnte gerade kein Spiel gezogen werden.</p>
        <button type="button" :disabled="starting" @click="retry()">Erneut versuchen</button>
      </div>
      <p v-else-if="entry === undefined && !drawing" class="state-message">
        Ziehe eine Karte. Du kannst so oft neu ziehen, wie du möchtest.
      </p>

      <div v-if="entry === null" class="state-message next-game-empty">
        <h2>{{ emptyState.title }}</h2>
        <p>{{ emptyState.hint }}</p>
        <button v-if="platformId" type="button" class="secondary" @click="showAllPlatforms()">
          Alle Plattformen
        </button>
        <NuxtLink v-else to="/" class="button secondary">Spiele entdecken</NuxtLink>
      </div>

      <article
        v-else-if="entry"
        :key="entry.gameId"
        class="next-game-card"
        :aria-busy="drawing || starting"
        aria-labelledby="next-game-title"
      >
        <GameCover :src="entry.display.coverUrl" :title="entry.display.title" />
        <div class="next-game-info">
          <p class="next-game-eyebrow">{{ started ? "Jetzt aktiv" : "Dein nächstes Spiel" }}</p>
          <h2 id="next-game-title">{{ entry.display.title }}</h2>
          <p class="muted">{{ ownershipLabels[entry.ownership] }} · {{ platformLabel(entry) }}</p>
          <p v-if="entry.catalogStatus === 'unavailable'" class="catalog-note">
            Katalog nicht erreichbar. Gespeicherte Spieldaten werden angezeigt.
          </p>
          <p v-else-if="entry.catalogStatus !== 'available'" class="catalog-note">
            Im Katalog nicht mehr verfügbar. Gespeicherte Spieldaten werden angezeigt.
          </p>
          <p v-if="onlyCandidate" class="notice">{{ onlyCandidateText }}</p>
          <p v-if="started" class="next-game-success">{{ startedText }}</p>
          <p v-if="startError" role="alert" class="error-message">{{ startError }}</p>
          <div class="next-game-actions">
            <button
              v-if="!started"
              ref="startButton"
              type="button"
              :disabled="starting || drawing"
              @click="startGame()"
            >
              {{ starting ? "Wird gestartet …" : "Jetzt spielen" }}
            </button>
            <NuxtLink v-else ref="dashboardLink" to="/dashboard" class="button"
              >Zum Dashboard</NuxtLink
            >
            <NuxtLink
              :to="{
                path: `/games/${entry.gameId}`,
                query: { returnTo: route.fullPath },
              }"
              class="button secondary"
              >Spieldetails</NuxtLink
            >
          </div>
        </div>
      </article>
    </div>
  </section>
</template>
