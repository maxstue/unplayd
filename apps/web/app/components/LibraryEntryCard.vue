<script setup lang="ts">
import {
  ownershipLabels,
  statusLabels,
  type LibraryEntryView,
  type PlayStatus,
} from "#shared/library";
defineProps<{ entry: LibraryEntryView; disabled?: boolean; saving?: boolean; returnTo: string }>();
const emit = defineEmits<{ status: [status: PlayStatus]; remove: [] }>();
</script>

<template>
  <article class="game-card library-card" :aria-busy="!!saving">
    <button
      type="button"
      class="card-remove"
      :aria-label="`${entry.display.title} entfernen`"
      :disabled="disabled"
      @click="emit('remove')"
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
        <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v5M14 11v5" />
      </svg>
    </button>
    <NuxtLink :to="{ path: `/games/${entry.gameId}`, query: { returnTo } }" class="game-link"
      ><GameCover :src="entry.display.coverUrl" :title="entry.display.title">
        <span v-if="saving" class="card-updating" role="status">Wird aktualisiert …</span>
      </GameCover>
      <h3>{{ entry.display.title }}</h3></NuxtLink
    >
    <p class="muted">
      {{ ownershipLabels[entry.ownership] }} ·
      {{
        entry.platformId
          ? (entry.game?.platforms.find((p) => p.id === entry.platformId)?.name ??
            "Plattform nicht verfügbar")
          : "Ohne Plattform"
      }}
    </p>
    <p v-if="entry.catalogStatus !== 'available'" class="muted">
      {{
        entry.catalogStatus === "unavailable"
          ? "Gespeicherte Spieldaten"
          : "Im Katalog nicht mehr verfügbar"
      }}
    </p>
    <label class="visually-hidden" :for="`status-${entry.gameId}`"
      >Spielstatus <span class="visually-hidden">für {{ entry.display.title }}</span></label
    >
    <div class="library-status-control">
      <select
        :id="`status-${entry.gameId}`"
        :value="entry.status"
        :disabled="disabled"
        @change="emit('status', ($event.target as HTMLSelectElement).value as PlayStatus)"
      >
        <option v-for="(label, value) in statusLabels" :key="value" :value="value">
          {{ label }}
        </option>
      </select>
      <svg
        class="select-chevron"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.7"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <path d="m7 10 5 5 5-5" />
      </svg>
    </div>
  </article>
</template>
