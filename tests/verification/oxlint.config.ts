import { defineConfig } from "oxlint";

export default defineConfig({
  plugins: ["typescript"],
  categories: { correctness: "error" },
  env: { node: true },
  ignorePatterns: ["node_modules/**"],
  rules: {
    "typescript/no-explicit-any": "error",
    "typescript/consistent-type-assertions": ["error", { assertionStyle: "never" }],
    "typescript/no-non-null-assertion": "error",
    "typescript/ban-ts-comment": ["error", {
      "ts-ignore": true,
      "ts-nocheck": true,
      "ts-expect-error": true,
      "ts-check": false
    }],
    "no-restricted-imports": ["error", {
      patterns: [{
        group: ["./*", "../*", "/*", "file:*"],
        message: "Use #verification/ imports for repository verification modules."
      }]
    }]
  }
});
