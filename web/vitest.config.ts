import { fileURLToPath, URL } from "node:url";

import { defineConfig } from "vitest/config";

// Unit tests cover pure store/state logic — Solid signals run headless under createRoot, so the node environment suffices (no DOM). The alias mirrors vite.config.ts so `@/` imports resolve identically (frontend.md). The browser condition selects Solid's client reactive core, whose resources and effects behave as in the shipped SPA rather than as in server rendering.
export default defineConfig({
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  ssr: { resolve: { conditions: ["browser"], externalConditions: ["browser"] } },
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});
