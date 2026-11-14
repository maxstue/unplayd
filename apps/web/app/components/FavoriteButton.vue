<script setup lang="ts">
defineProps<{ active: boolean; title: string; disabled?: boolean; label?: string }>();
const emit = defineEmits<{ toggle: [] }>();
</script>

<template>
  <button
    type="button"
    class="favorite-toggle"
    :class="{ active }"
    :aria-label="label ? undefined : `${title} als Favorit markieren`"
    :aria-pressed="active"
    :disabled="disabled"
    @click="emit('toggle')"
  >
    <svg
      viewBox="0 0 24 24"
      :fill="active ? 'currentColor' : 'none'"
      stroke="currentColor"
      stroke-width="1.7"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M12 20s-7-4.4-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.6-7 10-7 10Z" />
    </svg>
    <span v-if="label">{{ label }}</span>
  </button>
</template>
