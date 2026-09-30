import { readdirSync } from "node:fs";

import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

// Enforce @/ imports, downward FSD layers, and no sibling-slice imports. Generate sibling restrictions from current slice directories.

// forbidUp bans importing the given higher "@/<layer>" roots from a lower layer.
const forbidUp = (layers) =>
  layers.map((layer) => ({
    group: [`@/${layer}`, `@/${layer}/*`, `@/${layer}/**`],
    message: `FSD boundary: imports point downward only — this layer must not import @/${layer} (docs/conventions/frontend.md).`,
  }));

// noRelative bans every relative import path; use the "@/" alias instead.
const noRelative = {
  group: ["./*", "../*"],
  message: "Use the @/ alias, never a relative path (docs/conventions/frontend.md).",
};

const restrict = (upLayers) => [
  "error",
  { patterns: [noRelative, ...forbidUp(upLayers)] },
];

// slicesOf lists the folders directly under a layer — each is one slice.
const slicesOf = (layer) =>
  readdirSync(new URL(`./src/${layer}/`, import.meta.url), { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name);

// forbidSiblings bans every sibling slice of the layer, keeping the slice's own folder importable (a slice's files do reach each other through "@/").
const forbidSiblings = (layer, own) =>
  slicesOf(layer)
    .filter((slice) => slice !== own)
    .map((slice) => ({
      group: [`@/${layer}/${slice}`, `@/${layer}/${slice}/*`, `@/${layer}/${slice}/**`],
      message: `FSD boundary: a ${layer} slice must not import the sibling slice @/${layer}/${slice} — move shared code DOWN a layer (docs/conventions/frontend.md).`,
    }));

// sliceConfigs produces one config block per slice of a layer, re-stating the layer's own rules (a later block replaces the rule for those files).
const sliceConfigs = (layer, upLayers) =>
  slicesOf(layer).map((slice) => ({
    files: [`src/${layer}/${slice}/**/*.{ts,tsx}`],
    rules: {
      "@typescript-eslint/no-restricted-imports": [
        "error",
        { patterns: [noRelative, ...forbidUp(upLayers), ...forbidSiblings(layer, slice)] },
      ],
    },
  }));

export default tseslint.config(
  {
    ignores: ["dist/", "src/gen/**", "test-results/**", "playwright-report/**", "node_modules/**"],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.{ts,tsx}", "e2e/**/*.ts", "*.config.{ts,js}"],
    languageOptions: {
      globals: { ...globals.browser, ...globals.node },
    },
    rules: {
      "no-sequences": "error", // no comma operator (the (setOpen(o), reset()) idiom)
      "prefer-const": "error",
    },
  },
  { files: ["src/app/**/*.{ts,tsx}"], rules: { "@typescript-eslint/no-restricted-imports": restrict([]) } },
  { files: ["src/pages/**/*.{ts,tsx}"], rules: { "@typescript-eslint/no-restricted-imports": restrict(["app"]) } },
  { files: ["src/features/**/*.{ts,tsx}"], rules: { "@typescript-eslint/no-restricted-imports": restrict(["app", "pages"]) } },
  { files: ["src/entities/**/*.{ts,tsx}"], rules: { "@typescript-eslint/no-restricted-imports": restrict(["app", "pages", "features"]) } },
  { files: ["src/shared/**/*.{ts,tsx}"], rules: { "@typescript-eslint/no-restricted-imports": restrict(["app", "pages", "features", "entities"]) } },
  // Per-slice blocks come last so they replace the layer-wide rule for their files, adding the sibling ban to what that rule already forbids.
  ...sliceConfigs("features", ["app", "pages"]),
  ...sliceConfigs("entities", ["app", "pages", "features"]),
);
