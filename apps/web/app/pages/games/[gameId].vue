<script setup lang="ts">
import { kindLabels, type GameDetailView } from "#shared/catalog";
import { libraryReturnPath, ownershipLabels, type LibraryEntryView } from "#shared/library";
definePageMeta({ middleware: "auth", key: (route) => route.params.gameId as string });
const route = useRoute();
const returnTo = computed(() => libraryReturnPath(route.query.returnTo));
const backLabel = computed(() => {
  if (!returnTo.value) {
    return "Zurück zur Suche";
  }
  return returnTo.value.startsWith("/library") ? "Zurück zur Bibliothek" : "Zurück zum Dashboard";
});
const id = route.params.gameId as string;
const { data: detail, status, error, refresh } = useLazyFetch<GameDetailView>(`/api/games/${id}`);
const {
  data: entry,
  status: entryStatus,
  error: entryError,
  refresh: refreshEntry,
} = useLazyFetch<LibraryEntryView>(`/api/library/${id}`);
const knownEntries = useKnownEntries();
const ownership = ref<keyof typeof ownershipLabels>("owned");
const platformId = ref("");
const saving = ref(false);
const saveError = ref("");
const added = ref(false);
const missingEntry = computed(() => entryError.value?.statusCode === 404);
const personalSaving = ref(false);
const favoriteError = ref("");
const reviewError = ref("");
const reviewMessage = ref("");
const canWritePersonal = computed(
  () => !!entry.value || (missingEntry.value && !!detail.value?.game.isActive),
);
watch(
  entry,
  (value) => {
    if (value) {
      knownEntries.value[id] = true;
    }
  },
  { immediate: true },
);
useHead({ title: () => `${detail.value?.game.title ?? "Spieldetails"} · Unplayd` });

type PersonalChange = Partial<Pick<LibraryEntryView, "favorite" | "rating" | "note">>;
/**
 * Writes personal data that the user service keeps separate from the library state. Without an
 * entry, the first write creates it as wishlist/backlog, so the entry is loaded afterwards.
 */
async function writePersonal(
  path: string,
  method: "PUT" | "DELETE",
  body: Record<string, unknown> | undefined,
  change: PersonalChange,
) {
  if (personalSaving.value || !canWritePersonal.value) {
    return false;
  }
  personalSaving.value = true;
  try {
    await $fetch(path, { method, body });
    knownEntries.value[id] = true;
    if (entry.value) {
      entry.value = { ...entry.value, ...change };
    } else {
      added.value = true;
      await refreshEntry();
    }
    return true;
  } catch {
    return false;
  } finally {
    personalSaving.value = false;
  }
}

async function toggleFavorite() {
  const favorite = !entry.value?.favorite;
  favoriteError.value = "";
  if (
    !(await writePersonal(`/api/favorites/${id}`, favorite ? "PUT" : "DELETE", undefined, {
      favorite,
    }))
  ) {
    favoriteError.value = "Der Favorit konnte nicht gespeichert werden. Bitte versuche es erneut.";
  }
}

async function rate(rating: number | null) {
  reviewError.value = "";
  reviewMessage.value = "";
  const saved = await writePersonal(
    `/api/ratings/${id}`,
    rating === null ? "DELETE" : "PUT",
    rating === null ? undefined : { value: rating },
    { rating },
  );
  if (saved) {
    reviewMessage.value = rating === null ? "Bewertung entfernt." : "Bewertung gespeichert.";
  } else {
    reviewError.value = "Die Bewertung konnte nicht gespeichert werden. Bitte versuche es erneut.";
  }
}

async function saveNote(note: string | null) {
  reviewError.value = "";
  reviewMessage.value = "";
  const saved = await writePersonal(
    `/api/notes/${id}`,
    note === null ? "DELETE" : "PUT",
    note === null ? undefined : { text: note },
    { note },
  );
  if (saved) {
    reviewMessage.value = note === null ? "Notiz gelöscht." : "Notiz gespeichert.";
  } else {
    reviewError.value = "Die Notiz konnte nicht gespeichert werden. Bitte versuche es erneut.";
  }
}

async function addToLibrary() {
  if (
    saving.value ||
    entry.value ||
    added.value ||
    !missingEntry.value ||
    !detail.value?.game.isActive
  ) {
    return;
  }
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
</script>

<template>
  <section>
    <NuxtLink
      :to="returnTo ?? { path: '/', query: { query: route.query.query } }"
      class="back-link"
      >{{ backLabel }}</NuxtLink
    >
    <output v-if="status === 'pending'" class="state-message">
      Spieldetails werden geladen …
    </output>
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
            :to="{
              path: `/games/${detail.game.parent.id}`,
              query: { query: route.query.query, returnTo: returnTo ?? undefined },
            }"
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
          <div v-if="canWritePersonal && entryStatus !== 'pending'" class="favorite-action">
            <FavoriteButton
              :active="!!entry?.favorite"
              :title="detail.game.title"
              label="Favorit"
              :disabled="personalSaving || saving"
              @toggle="toggleFavorite()"
            />
            <p v-if="!entry && !added" class="muted">
              Fügt das Spiel als Wunschliste im Backlog zu deiner Bibliothek hinzu.
            </p>
            <p v-if="favoriteError" role="alert" class="error-message">{{ favoriteError }}</p>
          </div>
          <output v-if="entryStatus === 'pending'">Deine Bibliothek wird geprüft …</output>
          <div v-else-if="entry || added" class="notice">
            <h2>
              <output>{{ added ? "Zur Bibliothek hinzugefügt" : "In deiner Bibliothek" }}</output>
            </h2>
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
          <PersonalReview
            v-if="canWritePersonal && entryStatus !== 'pending'"
            :rating="entry?.rating ?? null"
            :note="entry?.note ?? null"
            :disabled="personalSaving || saving"
            :message="reviewMessage"
            :error="reviewError"
            @rate="rate($event)"
            @note="saveNote($event)"
          />
          <p v-if="canWritePersonal && !entry && !added" class="muted">
            Bewertung und Notiz fügen das Spiel als Wunschliste im Backlog hinzu.
          </p>
        </div>
      </div>
      <section v-if="detail.children.length" class="related-games" aria-labelledby="related-title">
        <h2 id="related-title">Editionen &amp; Zusatzinhalte</h2>
        <section v-for="group in detail.children" :key="group.kind" class="related-group">
          <h3>{{ kindLabels[group.kind] ?? group.kind }}</h3>
          <ul class="related-list">
            <li v-for="child in group.games" :key="child.id">
              <NuxtLink
                :to="{
                  path: `/games/${child.id}`,
                  query: { query: route.query.query, returnTo: returnTo ?? undefined },
                }"
                >{{ child.title }}</NuxtLink
              >
            </li>
          </ul>
        </section>
      </section>
    </template>
  </section>
</template>
