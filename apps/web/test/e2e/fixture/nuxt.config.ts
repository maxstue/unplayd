import { fileURLToPath } from "node:url";
import { defineNuxtConfig } from "nuxt/config";
import { useNuxt } from "nuxt/kit";

const fixture = (path: string) => fileURLToPath(new URL(path, import.meta.url));

// A separate test application extends the real pages, components, BFF and CSS.
// None of these substitutions is enabled by the production application.
export default defineNuxtConfig({
  extends: ["../../.."],
  css: [fixture("../../../app/assets/main.css"), fixture("../../../app/assets/library.css")],
  alias: {
    "@clerk/nuxt/composables": fixture("./clerk.ts"),
    "~": fixture("../../../app"),
    "@": fixture("../../../app"),
    "#shared": fixture("../../../shared"),
    [fixture("../../../server/middleware/02.clerk.ts")]: fixture("./session.ts"),
  },
  imports: { imports: [{ name: "useAuth", from: fixture("./clerk.ts") }] },
  runtimeConfig: {
    appOrigin: "http://127.0.0.1:9346",
    userApiUrl: "http://127.0.0.1:9347",
    catalogApiUrl: "http://127.0.0.1:9347",
    catalogServiceToken: "test-catalog-token-only-for-e2e-fixture",
    serviceTimeoutMs: 10_000,
    clerk: { secretKey: "sk_test_e2efixture" },
    public: { clerk: { publishableKey: "pk_test_Y2xlcmsuZXhhbXBsZSQ=" } },
  },
  hooks: {
    "modules:before"() {
      const nuxt = useNuxt();
      nuxt.options.modules = nuxt.options.modules.filter(
        (module) => module !== "@clerk/nuxt" && module !== "@nuxt/eslint",
      );
      for (const layer of nuxt.options._layers) {
        layer.config.modules = layer.config.modules?.filter(
          (module) => module !== "@clerk/nuxt" && module !== "@nuxt/eslint",
        );
      }
    },
  },
});
