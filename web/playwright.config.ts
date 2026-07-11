import { defineConfig } from "@playwright/test";

// Browser e2e against the REAL stack (docs/conventions/frontend.md, ADR-0013):
// the webServer script boots a throwaway Dockerized PostgreSQL and the Go
// binary serving the embedded SPA — a fresh database every run, so the
// first-run bootstrap flow is always exercised. Serial single-worker because
// bootstrap happens once per database and the scenarios build on each other.
export default defineConfig({
  testDir: "e2e",
  workers: 1,
  timeout: 60_000,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:18080",
  },
  webServer: {
    command: "sh e2e/server.sh",
    url: (process.env.E2E_BASE_URL ?? "http://127.0.0.1:18080") + "/readyz",
    reuseExistingServer: false,
    timeout: 180_000,
    // Playwright's default teardown is SIGKILL to the process group, which no
    // trap can catch — the script's `docker stop` cleanup would never run and
    // one PostgreSQL container would leak per run. SIGTERM lets the trap fire;
    // the timeout still SIGKILLs a hung shutdown.
    gracefulShutdown: { signal: "SIGTERM", timeout: 15_000 },
  },
});
