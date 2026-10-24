<script setup lang="ts">
definePageMeta({ middleware: "auth" });
const { data: user, error, refresh, status } = await useFetch("/api/me");
</script>

<template>
  <section>
    <h2>Dein Konto</h2>
    <output v-if="status === 'pending'">Dein Konto wird geladen …</output>
    <div v-else-if="error" role="alert">
      <p>Dein Konto konnte gerade nicht geladen werden.</p>
      <button type="button" @click="refresh()">Erneut versuchen</button>
    </div>
    <template v-else-if="user">
      <p>Du bist angemeldet. Dein Konto ist mit Unplayd verbunden.</p>
      <p class="account-id">Konto-ID: {{ user.id }}</p>
      <p>Hier entsteht deine persönliche Spielebibliothek.</p>
    </template>
  </section>
</template>
