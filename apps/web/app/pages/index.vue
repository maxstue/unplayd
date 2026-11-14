<script setup lang="ts">
const route = useRoute();
const router = useRouter();
const { isSignedIn, isLoaded } = useAuth();
const query = computed(() => (typeof route.query.query === "string" ? route.query.query : ""));
const enabled = computed(() => Boolean(isLoaded.value && isSignedIn.value));
const mounted = ref(false);
onMounted(() => {
  mounted.value = true;
});
const searchEnabled = computed(() => mounted.value && enabled.value);
const { items, status, nextCursor, loadingMore, moreError, retry, loadMore } = useGameSearch(
  query,
  searchEnabled,
  (term, cursor, signal) =>
    $fetch("/api/games", {
      query: { query: term, limit: 24, ...(cursor ? { cursor } : {}) },
      signal,
      retry: 0,
    }),
  0,
);
async function submitSearch(term: string) {
  if (term === query.value) {
    await retry();
    return;
  }
  await router.push({ path: "/", query: { ...route.query, query: term } });
}
async function openSuggestion(game: { id: string }, term: string) {
  await navigateTo({ path: `/games/${game.id}`, query: { query: term } });
}
const knownEntries = useKnownEntries();
useHead({ title: "Spiele entdecken · Unplayd" });
</script>

<template>
  <section aria-labelledby="discover-title">
    <div class="page-intro">
      <h1 id="discover-title">Finde dein nächstes Spiel.</h1>
      <p>Entdecke Spiele und mach sie zu deiner Sammlung.</p>
    </div>
    <template v-if="enabled">
      <GameSearchField
        :query="query"
        :enabled="enabled"
        @search="submitSearch"
        @select="openSuggestion"
      />
      <section
        class="search-results"
        aria-labelledby="results-title"
        :aria-busy="status === 'loading' || loadingMore"
      >
        <div class="results-heading">
          <h2 id="results-title">
            {{ query ? `Suchergebnisse für „${query}“` : "Suchergebnisse" }}
          </h2>
          <output v-if="status === 'success'" class="muted">
            {{ items.length }} {{ items.length === 1 ? "Spiel" : "Spiele"
            }}{{ nextCursor ? " · weitere verfügbar" : "" }}
          </output>
        </div>
        <output v-if="status === 'loading'" class="state-message muted">
          Spiele werden gesucht …
        </output>
        <div v-else-if="status === 'error'" role="alert" class="state-message">
          <p>Die Suche ist gerade nicht verfügbar. Bitte versuche es erneut.</p>
          <button type="button" @click="retry()">Erneut versuchen</button>
        </div>
        <output v-else-if="status === 'success' && !items.length" class="state-message muted">
          Keine Spiele gefunden. Versuche einen anderen Titel.
        </output>
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
    <output v-else-if="!isLoaded" class="state-message muted">Unplayd wird geladen …</output>
    <div v-else class="state-message">
      <p>Melde dich an, um Spiele zu suchen und deine eigene Bibliothek aufzubauen.</p>
      <NuxtLink class="button" to="/sign-in">Anmelden</NuxtLink>
      <NuxtLink class="button secondary" to="/sign-up">Konto erstellen</NuxtLink>
    </div>
  </section>
</template>
