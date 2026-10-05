import { defineConfig } from "@playwright/test";
import { createServer, type AddressInfo } from "node:net";
import { fileURLToPath } from "node:url";

/** Asks the OS for a free loopback TCP port and releases it for the harness to bind. */
async function reserveFreePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address: AddressInfo | string | null = server.address();
  await new Promise<void>((resolve) => server.close(() => resolve()));
  if (address === null || typeof address === "string") throw new Error("free port lookup returned no TCP address");
  return address.port;
}

// Worker processes reload this file and inherit the ports the main process assigned.
process.env.E2E_APP_PORT ??= String(await reserveFreePort());
process.env.E2E_TARGET_PORT ??= String(await reserveFreePort());
if (process.env.E2E_APP_PORT === process.env.E2E_TARGET_PORT) throw new Error("the application and target ports must differ");
const baseURL = `http://127.0.0.1:${process.env.E2E_APP_PORT}`;

// Run the real embedded SPA against a fresh PostgreSQL database with one worker because bootstrap and scenarios share installation state.
export default defineConfig({
  testDir: "e2e",
  workers: 1,
  timeout: 60_000,
  use: {
    baseURL,
    acceptDownloads: true,
    launchOptions: { downloadsPath: fileURLToPath(new URL("../.test-docker/downloads", import.meta.url)) },
  },
  webServer: {
    command: "sh e2e/server.sh",
    url: `${baseURL}/readyz`,
    reuseExistingServer: false,
    timeout: 180_000,
    gracefulShutdown: { signal: "SIGTERM", timeout: 15_000 },
  },
});
