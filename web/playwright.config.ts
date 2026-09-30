import { defineConfig } from "@playwright/test";

// Run the real embedded SPA against a fresh PostgreSQL database with one worker because bootstrap and scenarios share installation state.
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
    // Use SIGTERM so the server script can trap shutdown and remove its container; timeout still kills hung processes.
    gracefulShutdown: { signal: "SIGTERM", timeout: 15_000 },
  },
});
