import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./test/e2e",
  testMatch: "**/*.spec.ts",
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: [
    ["list"],
    ["html", { outputFolder: "coverage/e2e-report", open: "never" }],
    ["junit", { outputFile: "coverage/e2e-junit.xml" }],
  ],
  outputDir: "coverage/e2e-results",
  use: {
    baseURL: "http://127.0.0.1:9346",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      command: "node test/e2e/services.ts",
      url: "http://127.0.0.1:9347/health",
      reuseExistingServer: false,
    },
    {
      command: "nuxt build test/e2e/fixture && node test/e2e/fixture/.output/server/index.mjs",
      env: { HOST: "127.0.0.1", PORT: "9346" },
      url: "http://127.0.0.1:9346/api/health",
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
});
