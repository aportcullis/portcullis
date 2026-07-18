import { fileURLToPath, URL } from "node:url";

import { defineConfig } from "vitest/config";

// Unit tests cover pure store/state logic — Solid signals run headless under
// createRoot, so the node environment suffices (no DOM). The alias mirrors
// vite.config.ts so `@/` imports resolve identically (frontend.md).
export default defineConfig({
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});
