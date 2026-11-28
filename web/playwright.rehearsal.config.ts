import { defineConfig, devices } from "@playwright/test";

/**
 * LOCAL REHEARSAL ONLY (scripts/rehearse-prod.sh, docs/deploy.md). Runs against servers that
 * already exist: the web production build and the API behind a self-signed TLS proxy.
 * It starts nothing and ignores certificate errors, so never point it at a real deployment.
 *   REHEARSAL_WEB=https://parallel.localtest.me:18444 npx playwright test -c playwright.rehearsal.config.ts
 */
export default defineConfig({
  testDir: "./rehearsal",
  timeout: 120_000,
  expect: { timeout: 20_000 },
  workers: 1,
  reporter: [["list"]],
  outputDir: "./test-results-rehearsal",
  use: {
    ...devices["Desktop Chrome"],
    baseURL: process.env.REHEARSAL_WEB,
    ignoreHTTPSErrors: true,
  },
});
