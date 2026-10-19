export default defineNuxtConfig({
  compatibilityDate: "2026-10-15",
  telemetry: false,
  modules: ["@nuxt/eslint"],
  devtools: { enabled: false },
  runtimeConfig: {
    userApiUrl: "http://localhost:8081",
    catalogApiUrl: "http://localhost:8082",
    catalogServiceToken: "",
  },
  routeRules: {
    "/api/**": { headers: { "Cache-Control": "no-store" } },
  },
  typescript: { strict: true },
});
