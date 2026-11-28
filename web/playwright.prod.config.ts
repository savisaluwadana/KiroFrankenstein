import { defineConfig, devices } from "@playwright/test";

/**
 * PRODUCTION ACCEPTANCE against an already deployed stack (docs/deploy-railway-supabase-vercel.md, step 6).
 * Starts nothing and trusts only publicly valid certificates (no ignoreHTTPSErrors).
 *
 *   PROD_WEB_URL=https://<web> PROD_API_URL=https://<api> npm run e2e:prod
 *
 * PROD_API_URL is the API origin without a path (a trailing /api/v1 is tolerated).
 * PHASE=before | after splits the restart-persistence check around a manual `railway redeploy`:
 *   PHASE=before npm run e2e:prod   # runs everything and writes test-results/prod-phase.json
 *   (redeploy or restart the API)
 *   PHASE=after  npm run e2e:prod   # re-reads the recorded worlds and compares the state hashes
 * With no PHASE the suite runs once and the persistence check re-opens the worlds in a fresh browser context.
 * LLM calls: none, unless PROD_LLM_ACCESS_CODE is set (then at most 2 paid calls in one duel).
 * Creates a handful of small worlds (about 6) per run and never deletes anything.
 */
const web = process.env.PROD_WEB_URL;
const api = process.env.PROD_API_URL;
if (web === undefined || web === "" || api === undefined || api === "") {
  throw new Error("Set PROD_WEB_URL and PROD_API_URL (for example https://parallel.example.com and https://api.example.com).");
}

export default defineConfig({
  testDir: "./prod",
  testMatch: /.*\.spec\.ts/,
  timeout: 360_000,
  expect: { timeout: 20_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [["list"]],
  outputDir: "./test-results/prod",
  use: {
    baseURL: web.replace(/\/+$/, ""),
    trace: "retain-on-failure",
    reducedMotion: "no-preference",
  },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } } },
    {
      name: "mobile",
      use: { ...devices["Pixel 7"], viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true },
    },
  ],
});
