import withNuxt from "./.nuxt/eslint.config.mjs";
import oxlint from "eslint-plugin-oxlint";
import { fileURLToPath } from "node:url";

// Keep type-dependent rules here: Nuxt's vue-tsc check is not typed linting.
export default withNuxt()
  .append(oxlint.configs["flat/recommended"])
  .append({
    name: "unplayd/typed-promises",
    files: ["app/**/*.ts", "app/**/*.vue", "server/**/*.ts", "shared/**/*.ts", "test/**/*.ts"],
    languageOptions: {
      parserOptions: {
        project: [
          "./.nuxt/tsconfig.app.json",
          "./.nuxt/tsconfig.server.json",
          "./.nuxt/tsconfig.shared.json",
          "./tsconfig.test.json",
        ],
        tsconfigRootDir: fileURLToPath(new URL(".", import.meta.url)),
      },
    },
    rules: {
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/no-misused-promises": "error",
    },
  })
  .append({
    name: "unplayd/browser-boundary",
    files: ["app/**/*.{ts,vue}", "shared/**/*.ts"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["**/server/**", "#server/**", "#internal/**", "node:*"],
              message: "Browser/shared code cannot import server modules. Use a BFF endpoint.",
            },
          ],
        },
      ],
    },
  });
