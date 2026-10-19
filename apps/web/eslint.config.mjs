import withNuxt from "./.nuxt/eslint.config.mjs";
import oxlint from "eslint-plugin-oxlint";

// Oxlint handles the overlapping JS/TS rules; Nuxt retains Vue template rules.
export default withNuxt().append(oxlint.configs["flat/recommended"]);
