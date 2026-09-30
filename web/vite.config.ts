import { writeFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import { defineConfig, type Plugin } from "vite";
import solid from "vite-plugin-solid";

// emptyOutDir wipes the output dir (including the committed .gitkeep). Recreate
// it after the build so go:embed still compiles on a fresh clone that has not
// built the frontend.
function keepGitkeep(): Plugin {
  return {
    name: "keep-gitkeep",
    closeBundle() {
      const keep = fileURLToPath(
        new URL("../internal/platform/assets/dist/.gitkeep", import.meta.url),
      );
      writeFileSync(keep, "");
    },
  };
}

// The build output goes into the Go assets package so go:embed can bundle it
// into the single binary. In dev, API routes are proxied to the Go backend.
export default defineConfig({
  plugins: [solid(), tailwindcss(), keepGitkeep()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    outDir: "../internal/platform/assets/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/portcullis.v1": "http://localhost:8080",
      // Google login is a server-side redirect dance (ADR-0007), so the SPA's anchor must reach the Go routes in dev too.
      "/auth": "http://localhost:8080",
      "/livez": "http://localhost:8080",
      "/readyz": "http://localhost:8080",
    },
  },
});
