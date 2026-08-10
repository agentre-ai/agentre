import { defineConfig, devices } from "@playwright/test";
import { tmpdir } from "node:os";
import { join } from "node:path";

// Config for the LOCAL END-TO-END SYNC suite. Unlike playwright.config.ts it does
// NOT prepare anything itself: the data dir, the keychain dir, the real server,
// the seeded account and the cut-able proxy all come from run-e2e-sync.mjs, which
// must run first. Reject a bare `playwright test --config` early rather than boot
// an app that would quietly come up logged out and make every spec meaningless.
if (!process.env.AGENTRE_E2E_SERVER_URL || !process.env.SYNCE2E_BIN) {
  throw new Error(
    "playwright.sync.config.ts requires the sync harness env. Run `make e2e-sync` " +
      "(node run-e2e-sync.mjs), which starts agentre-server, seeds a throwaway account " +
      "and cleans it up afterwards.",
  );
}

const dataDir = process.env.AGENTRE_DATA_DIR ?? join(tmpdir(), "agentre-e2e-sync-data");
const DEVSERVER = "localhost:34216";
const BASE_URL = `http://${DEVSERVER}`;
const WEBSERVER_LOG = process.env.SYNCE2E_WEBSERVER_LOG ?? join(tmpdir(), "agentre-e2e-sync-webserver.log");

export default defineConfig({
  testDir: "./sync",
  // Sync is a 30 s poll loop; the local-path report only ever rides that ticker,
  // so a single spec legitimately spans more than one period.
  timeout: 240_000,
  expect: { timeout: 20_000 },
  fullyParallel: false,
  workers: 1,
  // One shared account, one shared desktop: the specs build on each other's rows
  // in order, so a retry of a single spec would start from the wrong state.
  retries: 0,
  reporter: [["list"]],
  use: {
    baseURL: BASE_URL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `wails dev -tags e2e -devserver ${DEVSERVER} > "${WEBSERVER_LOG}" 2>&1`,
    cwd: "..",
    url: BASE_URL,
    reuseExistingServer: false,
    timeout: 240_000,
    stdout: "ignore",
    stderr: "ignore",
    env: {
      AGENTRE_DATA_DIR: dataDir,
      AGENTRE_ENV: "test",
      AGENTRE_PROXY_PORT: "0",
      AGENTRE_E2E_KEYCHAIN_DIR: process.env.AGENTRE_E2E_KEYCHAIN_DIR as string,
      // The seeded login: the app comes up already connected to the throwaway
      // account, bypassing the GitHub-OAuth end of the device flow.
      AGENTRE_E2E_SERVER_URL: process.env.AGENTRE_E2E_SERVER_URL as string,
      AGENTRE_E2E_SERVER_USER_ID: process.env.AGENTRE_E2E_SERVER_USER_ID as string,
      AGENTRE_E2E_DEVICE_ID: process.env.AGENTRE_E2E_DEVICE_ID as string,
      AGENTRE_E2E_DEVICE_FINGERPRINT: process.env.AGENTRE_E2E_DEVICE_FINGERPRINT as string,
      AGENTRE_E2E_REFRESH_TOKEN: process.env.AGENTRE_E2E_REFRESH_TOKEN as string,
    },
  },
});
