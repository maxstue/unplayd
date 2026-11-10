<script setup lang="ts">
const { userId } = useAuth();
const route = useRoute();
const isAuthPage = computed(() => /^\/sign-(in|up)\/?$/.test(route.path));
const knownEntries = useState<Record<string, boolean>>("known-library-entries", () => ({}));
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
        <nav aria-label="Hauptnavigation">
          <NuxtLink
            to="/"
            :aria-current="
              $route.path === '/' || $route.path.startsWith('/games/') ? 'page' : undefined
            "
            >Spiele entdecken</NuxtLink
          >
          <Show when="signed-in"
            ><NuxtLink to="/dashboard">Dashboard</NuxtLink
            ><NuxtLink to="/library">Bibliothek</NuxtLink></Show
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
