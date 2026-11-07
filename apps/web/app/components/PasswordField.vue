<script setup lang="ts">
const model = defineModel<string>({ required: true });
defineProps<{ id: string; label: string; isNew?: boolean; disabled?: boolean }>();
const visible = ref(false);
</script>

<template>
  <div class="auth-field">
    <label :for="id">{{ label }}</label>
    <div class="password-field">
      <input
        :id="id"
        v-model="model"
        :type="visible ? 'text' : 'password'"
        :autocomplete="isNew ? 'new-password' : 'current-password'"
        :disabled="disabled"
        :minlength="isNew ? 8 : undefined"
        :aria-describedby="isNew ? `${id}-hint` : undefined"
        required
      />
      <button
        type="button"
        class="password-toggle"
        :aria-label="visible ? 'Passwort verbergen' : 'Passwort anzeigen'"
        :aria-pressed="visible"
        :disabled="disabled"
        @click="visible = !visible"
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          aria-hidden="true"
        >
          <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z" />
          <circle cx="12" cy="12" r="3" />
          <path v-if="visible" d="m3 3 18 18" />
        </svg>
      </button>
    </div>
    <p v-if="isNew" :id="`${id}-hint`" class="field-hint">
      Verwende ein langes, einzigartiges Passwort.
    </p>
  </div>
</template>
