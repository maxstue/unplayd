export default defineNuxtConfig({
  compatibilityDate: "2026-10-15",
  telemetry: false,
  app: { head: { title: "Unplayd", htmlAttrs: { lang: "de" } } },
  modules: ["@nuxt/eslint", "@clerk/nuxt"],
  clerk: {
    skipServerMiddleware: true,
    signInUrl: "/sign-in",
    signUpUrl: "/sign-up",
    signInFallbackRedirectUrl: "/dashboard",
    signUpFallbackRedirectUrl: "/dashboard",
  },
  devtools: { enabled: false },
  runtimeConfig: {
    appOrigin: "http://localhost:8444",
    userApiUrl: "http://localhost:8445",
    catalogApiUrl: "http://localhost:8446",
    catalogServiceToken: "",
  },
  routeRules: {
    "/": { headers: { "Cache-Control": "no-store" } },
    "/dashboard/**": { headers: { "Cache-Control": "no-store" } },
    "/sign-in/**": { headers: { "Cache-Control": "no-store" } },
    "/sign-up/**": { headers: { "Cache-Control": "no-store" } },
    "/api/**": { headers: { "Cache-Control": "no-store" } },
  },
  typescript: { strict: true },
});
