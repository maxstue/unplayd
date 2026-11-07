<script setup lang="ts">
import type { GameCardModel } from "../../shared/catalog";
defineProps<{ game: GameCardModel; inLibrary?: boolean }>();
const route = useRoute();
</script>

<template>
  <article class="game-card">
    <NuxtLink
      :to="{ path: `/games/${game.id}`, query: { query: route.query.query } }"
      class="game-link"
    >
      <GameCover :src="game.coverUrl" :title="game.title"
        ><span v-if="inLibrary" class="library-badge">In deiner Bibliothek</span></GameCover
      >
      <h3>{{ game.title }}</h3>
      <p class="muted">
        {{
          game.platforms
            .slice(0, 2)
            .map((platform) => platform.name)
            .join(" · ") || "Keine Plattformangabe"
        }}<template v-if="game.platforms.length > 2"> · +{{ game.platforms.length - 2 }}</template>
      </p>
    </NuxtLink>
    <slot />
  </article>
</template>
