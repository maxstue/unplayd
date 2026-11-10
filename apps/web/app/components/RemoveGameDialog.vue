<script setup lang="ts">
import type { LibraryEntryView } from "#shared/library";
const props = defineProps<{ entry: LibraryEntryView | null }>();
const emit = defineEmits<{ cancel: []; confirm: [id: string] }>();
const dialog = ref<HTMLDialogElement | null>(null);
watch(
  () => props.entry,
  async (entry) => {
    await nextTick();
    if (entry && !dialog.value?.open) dialog.value?.showModal();
    else if (!entry && dialog.value?.open) dialog.value.close();
  },
  { flush: "post" },
);
function confirm() {
  if (!props.entry) return;
  const id = props.entry.gameId;
  dialog.value?.close();
  emit("confirm", id);
}
function cancel() {
  dialog.value?.close();
  emit("cancel");
}
</script>

<template>
  <dialog
    ref="dialog"
    class="remove-dialog"
    aria-labelledby="remove-game-title"
    aria-describedby="remove-game-description"
    @cancel.prevent="cancel()"
    @click="$event.target === dialog && cancel()"
  >
    <div v-if="entry" class="remove-dialog-content">
      <button type="button" class="dialog-close" aria-label="Schließen" @click="cancel()">
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.7"
          aria-hidden="true"
        >
          <path d="m6 6 12 12M18 6 6 18" />
        </svg>
      </button>
      <div class="remove-dialog-main">
        <GameCover :src="entry.display.coverUrl" :title="entry.display.title" />
        <div>
          <h2 id="remove-game-title">Spiel entfernen?</h2>
          <p id="remove-game-description">
            „{{ entry.display.title }}“ wird aus deiner Bibliothek entfernt. Persönliche Daten zu
            diesem Spiel werden ebenfalls gelöscht.
          </p>
        </div>
      </div>
      <div class="dialog-actions">
        <button type="button" class="secondary" autofocus @click="cancel()">Abbrechen</button
        ><button type="button" class="danger" @click="confirm()">Spiel entfernen</button>
      </div>
    </div>
  </dialog>
</template>
