import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

// Flat ESLint config. Two conventions are machine-enforced here
// (docs/conventions/frontend.md):
//
//  1. Every import uses the "@/" alias — a relative path ("./" or "../") is
//     never allowed, including same-slice sibling files.
//  2. FSD layers point downward only: app → pages → features → entities →
//     shared → gen. A layer must not import a higher one.
//
// Both are matched on the import STRING, so no module resolver is needed. A
// relative path or an upward import now fails `make web-lint` instead of
// slipping through review. (Same-layer cross-slice — one entity importing
// another — cannot be expressed on the string alone while every import is
// "@/…"; it stays a review/convention item.)

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
);
