<script setup lang="ts">
const route = useRoute();
const router = useRouter();
const { isSignedIn, isLoaded } = useAuth();
const query = computed({
  get: () => (typeof route.query.query === "string" ? route.query.query : ""),
  set: (value: string) => {
    void router.replace({ query: { ...route.query, query: value || undefined } });
  },
});
const enabled = computed(() => Boolean(isLoaded.value && isSignedIn.value));
const { items, status, nextCursor, loadingMore, moreError, retry, loadMore } = useGameSearch(
  query,
  enabled,
  (term, cursor, signal) =>
    $fetch("/api/games", {
      query: { query: term, limit: 24, ...(cursor ? { cursor } : {}) },
      signal,
      retry: 0,
    }),
);
const knownEntries = useState<Record<string, boolean>>("known-library-entries", () => ({}));
useHead({ title: "Spiele entdecken · Unplayd" });
</script>

<template>
  <section aria-labelledby="discover-title">
    <div class="page-intro">
      <h1 id="discover-title">Finde dein nächstes Spiel.</h1>
      <p>Entdecke Spiele und mach sie zu deiner Sammlung.</p>
    </div>
    <template v-if="enabled">
      <form role="search" @submit.prevent="retry()">
        <label for="game-search">Spiele suchen</label>
        <div class="search-field">
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
          <input
            id="game-search"
            v-model="query"
            type="search"
            name="query"
            placeholder="Spielnamen eingeben …"
            maxlength="200"
            autocomplete="off"
            aria-describedby="search-hint"
          />
          <button v-if="query" type="button" aria-label="Suche leeren" @click="query = ''">
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
        </div>
        <p id="search-hint" class="muted" :class="{ 'visually-hidden': query.trim().length >= 2 }">
          Gib mindestens zwei Zeichen ein.
        </p>
      </form>
      <section
        class="search-results"
        aria-labelledby="results-title"
        :aria-busy="status === 'loading' || loadingMore"
      >
        <div class="results-heading">
          <h2 id="results-title">Suchergebnisse</h2>
          <p v-if="status === 'success'" class="muted" role="status">
            {{ items.length }} {{ items.length === 1 ? "Spiel" : "Spiele"
            }}{{ nextCursor ? " · weitere verfügbar" : "" }}
          </p>
        </div>
        <p v-if="status === 'loading'" role="status" class="state-message muted">
          Spiele werden gesucht …
        </p>
        <div v-else-if="status === 'error'" role="alert" class="state-message">
          <p>Die Suche ist gerade nicht verfügbar. Bitte versuche es erneut.</p>
          <button type="button" @click="retry()">Erneut versuchen</button>
        </div>
        <p
          v-else-if="status === 'success' && !items.length"
          role="status"
          class="state-message muted"
        >
          Keine Spiele gefunden. Versuche einen anderen Titel.
        </p>
        <p v-else-if="status === 'idle'" class="muted">Deine Suche beginnt hier.</p>
        <ul v-if="items.length" class="game-grid">
          <li v-for="hit in items" :key="hit.game.id">
            <GameCard :game="hit.game" :in-library="knownEntries[hit.game.id]">
              <details v-if="hit.matchedChildren.length" class="matched-children">
                <summary>{{ hit.matchedChildren.length }} passende Zusatzinhalte</summary>
                <ul>
                  <li v-for="child in hit.matchedChildren" :key="child.id">
                    <NuxtLink :to="{ path: `/games/${child.id}`, query: { query } }">{{
                      child.title
                    }}</NuxtLink>
                  </li>
                </ul>
              </details>
              <p v-else-if="hit.childCounts.length" class="muted">
                {{ hit.childCounts.reduce((count, group) => count + group.count, 0) }} Editionen
                &amp; Zusatzinhalte
              </p>
            </GameCard>
          </li>
        </ul>
        <div v-if="nextCursor" class="pagination">
          <p v-if="moreError" role="alert" class="error-message">
            Weitere Spiele konnten nicht geladen werden.
          </p>
          <button type="button" class="secondary" :disabled="loadingMore" @click="loadMore()">
            {{
              loadingMore ? "Wird geladen …" : moreError ? "Erneut laden" : "Weitere Spiele laden"
            }}
          </button>
        </div>
      </section>
    </template>
    <p v-else-if="!isLoaded" role="status" class="state-message muted">Unplayd wird geladen …</p>
    <div v-else class="state-message">
      <p>Melde dich an, um Spiele zu suchen und deine eigene Bibliothek aufzubauen.</p>
      <NuxtLink class="button" to="/sign-in">Anmelden</NuxtLink>
      <NuxtLink class="button secondary" to="/sign-up">Konto erstellen</NuxtLink>
    </div>
  </section>
</template>
