import { readdirSync } from "node:fs";
import { defineConfig, type OxlintConfig, type OxlintOverride } from "oxlint";

// Enforce @/ imports, downward FSD layers, and no sibling-slice imports. Generate sibling restrictions from current slice directories.

// forbidUp bans importing the given higher "@/<layer>" roots from a lower layer.
const forbidUp = (layers: string[]) =>
  layers.map((layer) => ({
    group: [`@/${layer}`, `@/${layer}/*`, `@/${layer}/**`],
    message: `FSD boundary: imports point downward only — this layer must not import @/${layer} (docs/conventions/frontend.md).`,
  }));

// noRelative bans every relative import path; use the "@/" alias instead.
const noRelative = {
  group: ["./*", "../*"],
  message: "Use the @/ alias, never a relative path (docs/conventions/frontend.md).",
};

const restrict = (upLayers: string[]): NonNullable<OxlintConfig["rules"]>["no-restricted-imports"] => [
  "error",
  { patterns: [noRelative, ...forbidUp(upLayers)] },
];

// slicesOf lists the folders directly under a layer — each is one slice.
const slicesOf = (layer: string) =>
  readdirSync(new URL(`./src/${layer}/`, import.meta.url), { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name);

// forbidSiblings bans every sibling slice of the layer, keeping the slice's own folder importable (a slice's files do reach each other through "@/").
const forbidSiblings = (layer: string, own: string) =>
  slicesOf(layer)
    .filter((slice) => slice !== own)
    .map((slice) => ({
      group: [`@/${layer}/${slice}`, `@/${layer}/${slice}/*`, `@/${layer}/${slice}/**`],
      message: `FSD boundary: a ${layer} slice must not import the sibling slice @/${layer}/${slice} — move shared code DOWN a layer (docs/conventions/frontend.md).`,
    }));

// sliceConfigs produces one config block per slice of a layer, re-stating the layer's own rules (a later block replaces the rule for those files).
const sliceConfigs = (layer: string, upLayers: string[]) =>
  slicesOf(layer).map<OxlintOverride>((slice) => ({
    files: [`src/${layer}/${slice}/**/*.{ts,tsx}`],
    rules: {
      "no-restricted-imports": [
        "error",
        { patterns: [noRelative, ...forbidUp(upLayers), ...forbidSiblings(layer, slice)] },
      ],
    },
  }));

export default defineConfig({
  plugins: ["typescript"],
  categories: { correctness: "off" },
  env: { browser: true, node: true },
  ignorePatterns: ["src/gen/**", "test-results/**", "playwright-report/**", "node_modules/**"],
  rules: {
    "constructor-super": "off",
    "for-direction": "error",
    "getter-return": "off",
    "no-async-promise-executor": "error",
    "no-case-declarations": "error",
    "no-class-assign": "off",
    "no-compare-neg-zero": "error",
    "no-cond-assign": "error",
    "no-const-assign": "off",
    "no-constant-binary-expression": "error",
    "no-constant-condition": "error",
    "no-control-regex": "error",
    "no-debugger": "error",
    "no-delete-var": "error",
    "no-dupe-class-members": "off",
    "no-dupe-else-if": "error",
    "no-dupe-keys": "off",
    "no-duplicate-case": "error",
    "no-empty": "error",
    "no-empty-character-class": "error",
    "no-empty-pattern": "error",
    "no-empty-static-block": "error",
    "no-ex-assign": "error",
    "no-extra-boolean-cast": "error",
    "no-fallthrough": "error",
    "no-func-assign": "off",
    "no-global-assign": "error",
    "no-import-assign": "off",
    "no-invalid-regexp": "error",
    "no-irregular-whitespace": "error",
    "no-loss-of-precision": "error",
    "no-misleading-character-class": "error",
    "no-new-native-nonconstructor": "off",
    "no-nonoctal-decimal-escape": "error",
    "no-obj-calls": "off",
    "no-prototype-builtins": "error",
    "no-redeclare": "off",
    "no-regex-spaces": "error",
    "no-self-assign": "error",
    "no-setter-return": "off",
    "no-shadow-restricted-names": "error",
    "no-sparse-arrays": "error",
    "no-this-before-super": "off",
    "no-unassigned-vars": "error",
    "no-undef": "off",
    "no-unexpected-multiline": "error",
    "no-unreachable": "off",
    "no-unsafe-finally": "error",
    "no-unsafe-negation": "off",
    "no-unsafe-optional-chaining": "error",
    "no-unused-labels": "error",
    "no-unused-private-class-members": "error",
    "no-unused-vars": "off",
    "no-useless-assignment": "error",
    "no-useless-backreference": "error",
    "no-useless-catch": "error",
    "no-useless-escape": "error",
    "no-with": "off",
    "preserve-caught-error": "error",
    "require-yield": "error",
    "use-isnan": "error",
    "valid-typeof": "error",
    "no-var": "error",
    "prefer-const": "error",
    "prefer-rest-params": "error",
    "prefer-spread": "error",
    "typescript/ban-ts-comment": "error",
    "no-array-constructor": "off",
    "typescript/no-array-constructor": "error",
    "typescript/no-duplicate-enum-values": "error",
    "typescript/no-empty-object-type": "error",
    "typescript/no-explicit-any": "error",
    // Types come from annotations, guards and parse functions, never from assertions; `as const` and `satisfies` stay available (docs/conventions/frontend.md).
    "typescript/consistent-type-assertions": ["error", { assertionStyle: "never" }],
    "typescript/no-non-null-assertion": "error",
    "typescript/no-extra-non-null-assertion": "error",
    "typescript/no-misused-new": "error",
    "typescript/no-namespace": "error",
    "typescript/no-non-null-asserted-optional-chain": "error",
    "typescript/no-require-imports": "error",
    "typescript/no-this-alias": "error",
    "typescript/no-unnecessary-type-constraint": "error",
    "typescript/no-unsafe-declaration-merging": "error",
    "typescript/no-unsafe-function-type": "error",
    "no-unused-expressions": "off",
    "typescript/no-unused-expressions": "error",
    "typescript/no-unused-vars": "error",
    "typescript/no-wrapper-object-types": "error",
    "typescript/prefer-as-const": "error",
    "typescript/prefer-namespace-keyword": "error",
    "typescript/triple-slash-reference": "error",
    "no-sequences": ["error", { "allowInParentheses": false }]
  },
  overrides: [
    { files: ["src/app/**/*.ts", "src/app/**/*.tsx"], rules: { "no-restricted-imports": restrict([]) } },
    { files: ["src/pages/**/*.ts", "src/pages/**/*.tsx"], rules: { "no-restricted-imports": restrict(["app"]) } },
    { files: ["src/features/**/*.ts", "src/features/**/*.tsx"], rules: { "no-restricted-imports": restrict(["app", "pages"]) } },
    { files: ["src/entities/**/*.ts", "src/entities/**/*.tsx"], rules: { "no-restricted-imports": restrict(["app", "pages", "features"]) } },
    { files: ["src/shared/**/*.ts", "src/shared/**/*.tsx"], rules: { "no-restricted-imports": restrict(["app", "pages", "features", "entities"]) } },
    ...sliceConfigs("features", ["app", "pages"]),
    ...sliceConfigs("entities", ["app", "pages", "features"]),
  ],
});
