<script setup lang="ts">
import { kindLabels, type GameDetailView } from "#shared/catalog";
definePageMeta({ middleware: "auth", key: (route) => route.params.gameId as string });
const route = useRoute();
const id = route.params.gameId as string;
const {
  data: detail,
  status,
  error,
  refresh,
} = await useLazyFetch<GameDetailView>(`/api/games/${id}`);
const {
  data: entry,
  status: entryStatus,
  error: entryError,
  refresh: refreshEntry,
} = await useLazyFetch(`/api/library/${id}`);
const knownEntries = useState<Record<string, boolean>>("known-library-entries", () => ({}));
const ownership = ref<"owned" | "wishlist" | "subscription">("owned");
const platformId = ref("");
const saving = ref(false);
const saveError = ref("");
const added = ref(false);
const missingEntry = computed(() => entryError.value?.statusCode === 404);
watch(
  entry,
  (value) => {
    if (value) knownEntries.value[id] = true;
  },
  { immediate: true },
);
useHead({ title: () => `${detail.value?.game.title ?? "Spieldetails"} · Unplayd` });

async function addToLibrary() {
  if (
    saving.value ||
    entry.value ||
    added.value ||
    !missingEntry.value ||
    !detail.value?.game.isActive
  )
    return;
  saving.value = true;
  saveError.value = "";
  try {
    await $fetch("/api/library", {
      method: "POST",
      body: { gameId: id, ownership: ownership.value, platformId: platformId.value || null },
    });
    knownEntries.value[id] = true;
    // Preserve a confirmed write even if the following catalog enrichment fails.
    added.value = true;
    await refreshEntry();
  } catch {
    saveError.value = "Das Spiel konnte nicht hinzugefügt werden. Bitte versuche es erneut.";
  } finally {
    saving.value = false;
  }
}
const ownershipLabels = { owned: "In Besitz", wishlist: "Wunschliste", subscription: "Abonnement" };
</script>

<template>
  <section>
    <NuxtLink :to="{ path: '/', query: { query: route.query.query } }" class="back-link"
      >Zurück zur Suche</NuxtLink
    >
    <p v-if="status === 'pending'" role="status" class="state-message">
      Spieldetails werden geladen …
    </p>
    <div v-else-if="error" role="alert" class="state-message">
      <h1>
        {{ error.statusCode === 404 ? "Spiel nicht gefunden" : "Spieldetails nicht verfügbar" }}
      </h1>
      <p>
        {{
          error.statusCode === 404
            ? "Dieses Spiel ist im Katalog nicht vorhanden."
            : "Der Katalog ist gerade nicht erreichbar. Bitte versuche es erneut."
        }}
      </p>
      <button v-if="error.statusCode !== 404" type="button" @click="refresh()">
        Erneut versuchen
      </button>
    </div>
    <template v-else-if="detail">
      <div class="detail-layout">
        <div class="detail-cover">
          <GameCover :src="detail.game.coverUrl" :title="detail.game.title" />
        </div>
        <div class="detail-main">
          <NuxtLink
            v-if="detail.game.parent"
            :to="{ path: `/games/${detail.game.parent.id}`, query: { query: route.query.query } }"
            >Hauptspiel: {{ detail.game.parent.title }}</NuxtLink
          >
          <h1>{{ detail.game.title }}</h1>
          <p class="platforms muted">
            {{
              detail.game.platforms.map((platform) => platform.name).join(" · ") ||
              "Keine Plattformangabe"
            }}
          </p>
          <p v-if="!detail.game.isActive" class="notice">
            Dieses Spiel ist im Katalog nicht mehr aktiv und kann nicht hinzugefügt werden.
          </p>
          <p v-if="entryStatus === 'pending'" role="status">Deine Bibliothek wird geprüft …</p>
          <div v-else-if="entry || added" class="notice" role="status">
            <h2>{{ added ? "Zur Bibliothek hinzugefügt" : "In deiner Bibliothek" }}</h2>
            <p v-if="entry">
              {{ ownershipLabels[entry.ownership] }} ·
              {{
                entry.platformId
                  ? (detail.game.platforms.find((platform) => platform.id === entry?.platformId)
                      ?.name ?? "Plattform nicht mehr verfügbar")
                  : "Ohne Plattformzuordnung"
              }}
            </p>
          </div>
          <div v-else-if="entryError && !missingEntry" role="alert" class="state-message">
            <p>
              Deine Bibliothek konnte nicht geprüft werden. Bitte lade sie erneut, bevor du das
              Spiel hinzufügst.
            </p>
            <button type="button" class="secondary" @click="refreshEntry()">
              Bibliothek erneut prüfen
            </button>
          </div>
          <form
            v-else-if="missingEntry && detail.game.isActive"
            class="library-form"
            @submit.prevent="addToLibrary()"
          >
            <h2>Zu deiner Bibliothek hinzufügen</h2>
            <div>
              <label for="ownership">Besitz</label
              ><select id="ownership" v-model="ownership" :disabled="saving">
                <option value="owned">In Besitz</option>
                <option value="wishlist">Wunschliste</option>
                <option value="subscription">Abonnement</option>
              </select>
            </div>
            <div>
              <label for="platform">Plattform</label
              ><select id="platform" v-model="platformId" :disabled="saving">
                <option value="">Keine Plattform auswählen</option>
                <option
                  v-for="platform in detail.game.platforms"
                  :key="platform.id"
                  :value="platform.id"
                >
                  {{ platform.name }}
                </option>
              </select>
            </div>
            <button type="submit" :disabled="saving">
              {{ saving ? "Wird hinzugefügt …" : "Zur Bibliothek hinzufügen" }}
            </button>
          </form>
          <p v-if="saveError" role="alert" class="error-message">{{ saveError }}</p>
        </div>
      </div>
      <section v-if="detail.children.length" class="related-games" aria-labelledby="related-title">
        <h2 id="related-title">Editionen &amp; Zusatzinhalte</h2>
        <section v-for="group in detail.children" :key="group.kind" class="related-group">
          <h3>{{ kindLabels[group.kind] ?? group.kind }}</h3>
          <ul class="related-list">
            <li v-for="child in group.games" :key="child.id">
              <NuxtLink :to="{ path: `/games/${child.id}`, query: { query: route.query.query } }">{{
                child.title
              }}</NuxtLink>
            </li>
          </ul>
        </section>
      </section>
    </template>
  </section>
</template>
