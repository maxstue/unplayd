import { defineConfig } from "vite-plus";

export default defineConfig({
  test: {
    include: ["apps/web/test/**/*.test.ts"],
    environment: "node",
    reporters: ["default", "junit"],
    outputFile: { junit: "apps/web/coverage/junit.xml" },
    coverage: {
      provider: "v8",
      include: ["apps/web/server/**/*.ts", "apps/web/shared/**/*.ts"],
      reportsDirectory: "apps/web/coverage",
      reporter: ["text", "lcov", "json-summary"],
    },
  },
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
    rules: {
      "vite-plus/prefer-vite-plus-imports": "error",
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: [
                "**/apps/user-api/**",
                "**/apps/catalog-api/**",
                "@unplayd/user-api",
                "@unplayd/catalog-api",
                "**/../user-api/**",
                "**/../catalog-api/**",
              ],
              message: "Apps communicate through HTTP contracts, not source imports.",
            },
          ],
        },
      ],
    },
    ignorePatterns: ["**/generated/**", "**/.nuxt/**", "**/.output/**", "**/build/**"],
    // vue-tsc owns type checking; complementary ESLint owns type-aware rules.
    options: { typeAware: false, typeCheck: false },
  },
});
