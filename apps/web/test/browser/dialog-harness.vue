<script setup lang="ts">
import { ref } from "vue";
import RemoveGameDialog from "../../app/components/RemoveGameDialog.vue";
import type { LibraryEntryView } from "../../shared/library";
defineProps<{ entry: LibraryEntryView }>();
const selected = ref<LibraryEntryView | null>(null);
const result = ref("");
</script>

<template>
  <button @click="selected = entry">Entfernen öffnen</button>
  <output>{{ result }}</output>
  <RemoveGameDialog
    :entry="selected"
    @cancel="
      selected = null;
      result = 'Abgebrochen';
    "
    @confirm="
      selected = null;
      result = $event;
    "
  />
</template>
