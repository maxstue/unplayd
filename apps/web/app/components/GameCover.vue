<script setup lang="ts">
const props = defineProps<{ src: string | null; title: string }>();
const failed = ref(false);
watch(
  () => props.src,
  () => {
    failed.value = false;
  },
);
</script>

<template>
  <div class="game-cover">
    <img
      v-if="src && !failed"
      :src="src"
      alt=""
      loading="lazy"
      width="264"
      height="352"
      referrerpolicy="no-referrer"
      @error="failed = true"
    />
    <span v-else class="cover-fallback" aria-hidden="true">{{ title }}</span>
    <slot />
  </div>
</template>
