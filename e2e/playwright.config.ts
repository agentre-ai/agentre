import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.AGENTRE_E2E_BASE_URL;
const outputDir = process.env.AGENTRE_E2E_PLAYWRIGHT_DIR;

if (!baseURL || !outputDir || !process.env.AGENTRE_DATA_DIR) {
  throw new Error("Playwright must be started by e2e/run-e2e.mjs");
}

export default defineConfig({
  testDir: "./tests",
  testMatch: ["desktop.spec.ts", "sync-client.spec.ts", "remote-peer.spec.ts"],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  outputDir,
  reporter: process.env.CI
    ? [
        ["list"],
        ["html", { open: "never" }],
      ]
    : [["list"]],
  use: {
    baseURL,
    headless: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
