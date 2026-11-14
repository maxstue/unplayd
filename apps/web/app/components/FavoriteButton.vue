<script setup lang="ts">
defineProps<{ active: boolean; title: string; disabled?: boolean; label?: string }>();
const emit = defineEmits<{ toggle: [] }>();
const popping = ref(false);
function toggle() {
  // Restart the animation on repeated clicks.
  popping.value = false;
  requestAnimationFrame(() => {
    popping.value = true;
  });
  emit("toggle");
}
</script>

<template>
  <button
    type="button"
    class="favorite-toggle"
    :class="{ active, popping }"
    :aria-label="label ? undefined : `${title} als Favorit markieren`"
    :aria-pressed="active"
    :disabled="disabled"
    @click="toggle()"
    @animationend="popping = false"
  >
    <svg
      viewBox="0 0 24 24"
      fill="currentColor"
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
