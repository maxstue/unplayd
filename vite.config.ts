import { defineConfig } from "vite-plus";

export default defineConfig({
  // Nuxt owns the app's Vite config. This root config is for workspace tooling.
  fmt: {
    ignorePatterns: [
      "pnpm-lock.yaml",
      "**/generated/**",
      "**/build/**",
      "**/.gradle/**",
      "**/.nuxt/**",
      "**/.output/**",
      "coverage/**",
    ],
  },
  lint: {
    plugins: ["typescript", "unicorn", "oxc", "vue"],
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
    rules: { "vite-plus/prefer-vite-plus-imports": "error" },
    ignorePatterns: ["**/generated/**", "**/.nuxt/**", "**/.output/**", "**/build/**"],
    // nuxt typecheck runs vue-tsc, including Vue templates and Nuxt-generated types.
    options: { typeAware: false, typeCheck: false },
  },
});
