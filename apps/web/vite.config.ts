import { defineConfig } from "vite-plus";
import { fileURLToPath } from "node:url";

export default defineConfig({
  test: {
    include: ["test/**/*.test.ts"],
    environment: "node",
    reporters: ["default", "junit"],
    outputFile: { junit: "coverage/junit.xml" },
    coverage: {
      provider: "v8",
      include: ["server/**/*.ts", "shared/**/*.ts"],
      reportsDirectory: "coverage",
      reporter: [
        "text",
        // Sonar imports LCOV from the repository root.
        ["lcov", { projectRoot: fileURLToPath(new URL("../../", import.meta.url)) }],
        "json-summary",
      ],
    },
  },
  // Nuxt owns dev/build configuration; this file configures web lint, format and tests.
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
                "@unplayd/user-api",
                "@unplayd/catalog-api",
                "**/user-api/**",
                "**/catalog-api/**",
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
