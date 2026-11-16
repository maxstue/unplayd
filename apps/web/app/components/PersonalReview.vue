<script setup lang="ts">
import { noteMaxLength } from "#shared/library";
const props = defineProps<{
  rating: number | null;
  note: string | null;
  disabled?: boolean;
  message?: string;
  error?: string;
}>();
/** `null` removes the rating or note. */
const emit = defineEmits<{ rate: [value: number | null]; note: [text: string | null] }>();
const selected = ref(props.rating);
// Show the saved rating again once a write settles, including after a failed write.
watch([() => props.rating, () => props.disabled], ([rating, disabled]) => {
  if (!disabled) {
    selected.value = rating;
  }
});
const draft = ref(props.note ?? "");
// A saved note replaces the draft; unsaved edits survive other updates such as a new rating.
watch(
  () => props.note,
  (note) => {
    draft.value = note ?? "";
  },
);
// Counts code points like the user service; the textarea's maxlength would count UTF-16 units.
const length = computed(() => [...draft.value].length);
const tooLong = computed(() => length.value > noteMaxLength);
const savable = computed(
  () => !tooLong.value && draft.value.trim() !== "" && draft.value !== props.note,
);
const formatter = new Intl.NumberFormat("de-DE");

function rate(event: Event) {
  const value = (event.target as HTMLSelectElement).value;
  selected.value = value ? Number(value) : null;
  emit("rate", selected.value);
}
function save() {
  if (!props.disabled && savable.value) {
    emit("note", draft.value);
  }
}
</script>

<template>
  <section class="personal-review" aria-labelledby="personal-review-title">
    <h2 id="personal-review-title">Deine Bewertung</h2>
    <div>
      <label for="rating">Bewertung (1–10)</label>
      <select id="rating" :value="selected ?? ''" :disabled="disabled" @change="rate($event)">
        <option value="">Keine Bewertung</option>
        <option v-for="value in 10" :key="value" :value="value">{{ value }} / 10</option>
      </select>
    </div>
    <form @submit.prevent="save()">
      <label for="note">Persönliche Notiz</label>
      <textarea
        id="note"
        v-model="draft"
        rows="5"
        :disabled="disabled"
        :aria-invalid="tooLong"
        aria-describedby="note-length"
      />
      <p id="note-length" class="muted" :class="{ 'error-message': tooLong }">
        {{ formatter.format(length) }} / {{ formatter.format(noteMaxLength) }} Zeichen
      </p>
      <div class="personal-review-actions">
        <button type="submit" :disabled="disabled || !savable">Notiz speichern</button>
        <button
          v-if="note"
          type="button"
          class="secondary"
          :disabled="disabled"
          @click="emit('note', null)"
        >
          Notiz löschen
        </button>
      </div>
    </form>
    <output v-if="message">{{ message }}</output>
    <p v-if="error" role="alert" class="error-message">{{ error }}</p>
  </section>
</template>
