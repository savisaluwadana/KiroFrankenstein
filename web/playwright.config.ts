import { defineConfig, devices } from "@playwright/test";

/**
 * Real-browser tests against the real Go API (in-memory store) and a production
 * build of the web app. Ports are chosen to avoid the usual local dev ports.
 */
const WEB_PORT = 3199;
const API_PORT = 18080;
const API_BASE = `http://localhost:${API_PORT}/api/v1`;

export default defineConfig({
  testDir: "./e2e",
  testMatch: /.*\.spec\.ts/,
  timeout: 180_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  // Fail fast on CI so the job reports real errors instead of being cancelled by its timeout.
  maxFailures: process.env.CI ? 3 : 0,
  reporter: process.env.CI ? [["list"], ["github"]] : [["list"]],
  outputDir: "./test-results",
  use: {
    baseURL: `http://localhost:${WEB_PORT}`,
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
  webServer: [
    {
      command: `go build -o /tmp/parallel-api-e2e ./cmd/parallel-api && PORT=${API_PORT} WEB_ORIGIN=http://localhost:${WEB_PORT} RATE_LIMIT_RPS=1000 RATE_LIMIT_BURST=2000 /tmp/parallel-api-e2e`,
      cwd: "../backend",
      url: `${API_BASE}/health`,
      reuseExistingServer: process.env.PW_REUSE === "1",
      timeout: 180_000,
    },
    {
      // NEXT_PUBLIC_* is inlined at build time, so it must be set for the build.
      command: `NEXT_PUBLIC_API_BASE_URL=${API_BASE} npm run build && NEXT_PUBLIC_API_BASE_URL=${API_BASE} npx next start -p ${WEB_PORT}`,
      url: `http://localhost:${WEB_PORT}`,
      reuseExistingServer: process.env.PW_REUSE === "1",
      timeout: 600_000,
    },
  ],
});
