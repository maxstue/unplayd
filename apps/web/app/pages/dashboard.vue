<script setup lang="ts">
import type { LibraryPageView, LibraryOverview } from "#shared/library";
definePageMeta({ middleware: "auth" });
useHead({ title: "Dashboard · Unplayd" });
const {
  data: overview,
  error: overviewError,
  refresh: refreshOverview,
} = await useLazyFetch<LibraryOverview>("/api/library/overview");
const {
  data: playing,
  status,
  error,
  refresh,
} = await useLazyFetch<LibraryPageView>("/api/library", { query: { status: "playing", limit: 3 } });
const {
  data: backlog,
  status: backlogStatus,
  error: backlogError,
  refresh: refreshBacklog,
} = await useLazyFetch<LibraryPageView>("/api/library", { query: { status: "backlog", limit: 6 } });
const summary = computed(() => [
  {
    label: "Spiele insgesamt",
    count: overview.value?.total,
    status: undefined,
    icon: "collection",
  },
  {
    label: "Gerade aktiv",
    count: overview.value?.statuses.playing,
    status: "playing",
    icon: "play",
  },
  {
    label: "Im Backlog",
    count: overview.value?.statuses.backlog,
    status: "backlog",
    icon: "bookmark",
  },
  {
    label: "Abgeschlossen",
    count: overview.value?.statuses.completed,
    status: "completed",
    icon: "check",
  },
]);
</script>

<template>
  <section class="dashboard-view">
    <div class="dashboard-heading">
      <div>
        <h1>Dein Dashboard</h1>
        <p>Deine Sammlung. Dein nächstes Abenteuer.</p>
      </div>
      <NuxtLink to="/" class="button"
        ><svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.7"
          aria-hidden="true"
        >
          <path d="M12 5v14M5 12h14" />
        </svg>
        Spiel hinzufügen</NuxtLink
      >
    </div>
    <div v-if="overviewError" class="notice" role="alert">
      Die Übersicht ist gerade nicht verfügbar.
      <button type="button" class="text-button" @click="refreshOverview()">Erneut versuchen</button>
    </div>
    <nav v-else class="collection-summary" aria-label="Deine Sammlung nach Status">
      <NuxtLink
        v-for="item in summary"
        :key="item.label"
        :to="{ path: '/library', query: { status: item.status } }"
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.7"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path
            v-if="item.icon === 'collection'"
            d="m12 3 9 5-9 5-9-5 9-5ZM3 12l9 5 9-5M3 16l9 5 9-5"
          />
          <path v-else-if="item.icon === 'play'" d="m8 4 12 8-12 8V4Z" />
          <path v-else-if="item.icon === 'bookmark'" d="M6 4h12v17l-6-4-6 4V4Z" />
          <path v-else d="m5 12 4 4L19 6" /></svg
        ><span
          ><strong>{{ item.count ?? "—" }}</strong> {{ item.label }}</span
        >
      </NuxtLink>
    </nav>
    <div class="dashboard-layout">
      <DashboardGameSection
        title="Weiterspielen"
        :page="playing"
        :pending="status === 'pending'"
        :failed="!!error"
        status="playing"
        compact
        @retry="refresh()"
      />
      <aside class="dashboard-panel collection-panel" aria-labelledby="collection-title">
        <h2 id="collection-title">Deine Bibliothek</h2>
        <p>Alles, was du spielst. Und alles, was noch auf dich wartet.</p>
        <nav aria-label="Bibliothek entdecken">
          <NuxtLink to="/library"
            >Alle Spiele
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.7"
              aria-hidden="true"
            >
              <path d="m9 5 7 7-7 7" /></svg></NuxtLink
          ><NuxtLink :to="{ path: '/library', query: { status: 'paused' } }"
            >Pausiert <span v-if="overview">{{ overview.statuses.paused }}</span></NuxtLink
          ><NuxtLink :to="{ path: '/library', query: { status: 'completed' } }"
            >Abgeschlossen <span v-if="overview">{{ overview.statuses.completed }}</span></NuxtLink
          ><NuxtLink to="/"
            >Spiele entdecken
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.7"
              aria-hidden="true"
            >
              <path d="m9 5 7 7-7 7" /></svg
          ></NuxtLink>
        </nav>
      </aside>
    </div>
    <DashboardGameSection
      title="Aus deinem Backlog"
      subtitle="Dein nächstes Abenteuer wartet schon."
      :page="backlog"
      :pending="backlogStatus === 'pending'"
      :failed="!!backlogError"
      status="backlog"
      @retry="refreshBacklog()"
    />
  </section>
</template>
