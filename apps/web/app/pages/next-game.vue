<script setup lang="ts">
import { libraryFilters, ownershipLabels, platformLabel, type NextGameView } from "#shared/library";
definePageMeta({ middleware: "auth" });
useHead({ title: "Nächstes Spiel · Unplayd" });
const route = useRoute();
const router = useRouter();
const platformId = computed(() => libraryFilters(route.query).platformId);
const {
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
} = useNextGame(platformId, {
  draw: (query, signal) => $fetch<NextGameView>("/api/next-game", { query, signal }),
  start: (id) =>
    $fetch(`/api/library/${id}/status`, { method: "PUT", body: { status: "playing" } }),
});
const {
  data: platforms,
  error: platformsError,
  refresh: refreshPlatforms,
} = useLazyFetch("/api/library/platforms");
const announcement = computed(() => {
  if (drawing.value) {
    return "Spiel wird gezogen …";
  }
  if (!drawn.value) {
    return "";
  }
  if (!entry.value) {
    return platformId.value ? "Kein Backlog-Spiel für diese Plattform." : "Dein Backlog ist leer.";
  }
  const title = `„${entry.value.display.title}“`;
  if (started.value) {
    return `Viel Spaß! ${title} steht jetzt auf „Spiele ich gerade“.`;
  }
  return onlyCandidate.value
    ? `${title} ist das einzige passende Spiel in deinem Backlog.`
    : `${title} gezogen.`;
});
function selectPlatform(value: string) {
  return router.replace({ path: "/next-game", query: { platformId: value || undefined } });
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
      <button type="button" class="draw-button" @click="draw()">
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
          <circle cx="9" cy="9" r="1" fill="currentColor" />
          <circle cx="15" cy="15" r="1" fill="currentColor" />
          <circle cx="15" cy="9" r="1" fill="currentColor" />
          <circle cx="9" cy="15" r="1" fill="currentColor" />
        </svg>
        {{ drawn && entry ? "Neu ziehen" : "Spiel ziehen" }}
      </button>
    </div>
    <div v-if="platformsError" role="alert" class="notice">
      Plattformauswahl konnte nicht geladen werden.
      <button type="button" class="text-button" @click="refreshPlatforms()">Erneut laden</button>
    </div>

    <output class="visually-hidden" aria-live="polite">{{ announcement }}</output>
    <div class="next-game-stage">
      <div
        v-if="!drawn || (drawing && !entry)"
        class="card-stack"
        :class="{ shuffling: drawing }"
        aria-hidden="true"
      >
        <span /><span /><span />
      </div>

      <div v-if="failed" role="alert" class="state-message">
        <p>Es konnte gerade kein Spiel gezogen werden.</p>
        <button type="button" @click="draw()">Erneut versuchen</button>
      </div>
      <p v-else-if="!drawn && !drawing" class="state-message">
        Ziehe eine Karte. Du kannst so oft neu ziehen, wie du möchtest.
      </p>

      <div v-if="drawn && !entry" class="state-message next-game-empty">
        <h2>
          {{ platformId ? "Kein Backlog-Spiel für diese Plattform" : "Dein Backlog ist leer" }}
        </h2>
        <p>
          {{
            platformId
              ? "Wähle eine andere Plattform oder ziehe aus allen Plattformen."
              : "Füge Spiele mit dem Status „Backlog“ hinzu, damit hier etwas auf dich wartet."
          }}
        </p>
        <button v-if="platformId" type="button" class="secondary" @click="selectPlatform('')">
          Alle Plattformen
        </button>
        <NuxtLink v-else to="/" class="button secondary">Spiele entdecken</NuxtLink>
      </div>

      <article
        v-else-if="entry"
        :key="reveal"
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
          <p v-if="onlyCandidate" class="notice">
            Das ist das einzige passende Spiel in deinem Backlog.
          </p>
          <p v-if="started" class="next-game-success">
            Viel Spaß! Das Spiel steht jetzt auf „Spiele ich gerade“.
          </p>
          <p v-if="startError" role="alert" class="error-message">{{ startError }}</p>
          <div class="next-game-actions">
            <button v-if="!started" type="button" :disabled="starting || drawing" @click="start()">
              {{ starting ? "Wird gestartet …" : "Jetzt spielen" }}
            </button>
            <NuxtLink v-else to="/dashboard" class="button">Zum Dashboard</NuxtLink>
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
