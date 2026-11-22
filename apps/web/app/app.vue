<script setup lang="ts">
const { userId } = useAuth();
const route = useRoute();
const isAuthPage = computed(() => /^\/sign-(in|up)\/?$/.test(route.path));
// Shared attributes for the decorative navigation icons, shown only in the mobile tab bar.
const icon = {
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  "stroke-width": 1.7,
  "stroke-linecap": "round",
  "stroke-linejoin": "round",
  "aria-hidden": "true",
} as const;
const knownEntries = useKnownEntries();
watch(userId, (current, previous) => {
  if (previous && current !== previous) {
    knownEntries.value = {};
    clearNuxtData();
    void navigateTo("/");
  }
});
</script>

<template>
  <div class="app-shell">
    <a class="skip-link" href="#main-content">Zum Inhalt</a>
    <header class="app-header">
      <div class="header-content">
        <NuxtLink to="/" class="brand">Unplayd<span aria-hidden="true">.</span></NuxtLink>
        <nav aria-label="Hauptnavigation" class="main-nav" :class="{ 'tab-bar': userId }">
          <NuxtLink
            to="/"
            :aria-current="
              $route.path === '/' || $route.path.startsWith('/games/') ? 'page' : undefined
            "
            ><svg v-bind="icon">
              <circle cx="12" cy="12" r="9" />
              <path d="m15.5 8.5-2 5-5 2 2-5 5-2Z" /></svg
            >Entdecken</NuxtLink
          >
          <Show when="signed-in"
            ><NuxtLink to="/dashboard"
              ><svg v-bind="icon">
                <path d="M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z" /></svg
              >Dashboard</NuxtLink
            ><NuxtLink to="/library"
              ><svg v-bind="icon"><path d="m12 3 9 5-9 5-9-5 9-5ZM3 12l9 5 9-5M3 16l9 5 9-5" /></svg
              >Bibliothek</NuxtLink
            ><NuxtLink to="/next-game"
              ><svg v-bind="icon">
                <rect x="4" y="4" width="16" height="16" rx="3" />
                <path d="M9 9h.01M15 9h.01M9 15h.01M15 15h.01" stroke-width="2.5" /></svg
              >Nächstes Spiel</NuxtLink
            ></Show
          >
        </nav>
        <Show when="signed-out">
          <NuxtLink v-if="!isAuthPage" to="/sign-in" class="button secondary">Anmelden</NuxtLink>
        </Show>
        <Show when="signed-in"><UserButton /></Show>
      </div>
    </header>
    <main id="main-content" class="page-content" tabindex="-1"><NuxtPage /></main>
    <footer class="app-footer">
      <div>
        Spieldaten von
        <a href="https://www.igdb.com/" target="_blank" rel="noopener noreferrer">IGDB</a>
      </div>
    </footer>
  </div>
</template>
