import { defineConfig } from "vite-plus";
import vue from "@vitejs/plugin-vue";
import { playwright } from "vite-plus/test/browser-playwright";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { "#shared": fileURLToPath(new URL("./shared", import.meta.url)) },
  },
  test: {
    include: ["test/browser/**/*.test.ts"],
    setupFiles: ["./test/browser/setup.ts"],
    reporters: ["default", "junit"],
    outputFile: { junit: "coverage/browser-junit.xml" },
    browser: {
      enabled: true,
      headless: true,
      provider: playwright(),
      instances: [{ browser: "chromium" }],
    },
  },
});
