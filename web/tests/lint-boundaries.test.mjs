import assert from "node:assert/strict";
import { writeFileSync, unlinkSync } from "node:fs";
import { spawnSync } from "node:child_process";
import test from "node:test";

const scenarios = [
  ["downward alias import is allowed", "features/request", 'export { value } from "@/shared/example";', null],
  ["relative imports are rejected", "features/request", 'export { value } from "./example";', "no-restricted-imports"],
  ["lower layers cannot import pages", "shared", 'export { value } from "@/pages/example";', "no-restricted-imports"],
  ["sibling feature slices are rejected", "features/request", 'export { value } from "@/features/auth/example";', "no-restricted-imports"],
  ["type-only imports obey layer boundaries", "shared", 'export type { Value } from "@/pages/example";', "no-restricted-imports"],
  ["explicit any remains rejected", "shared", "export type Value = any;", "no-explicit-any"],
  ["comma operators remain rejected", "shared", "export const value = (1, 2);", "no-sequences"],
];

for (const [name, slice, source, rule] of scenarios) {
  test(name, () => {
    const path = `src/${slice}/lintScenario${process.pid}.ts`;
    writeFileSync(path, source, { flag: "wx" });
    try {
      const args = ["exec", "oxlint", "--deny-warnings"];
      if (process.env.LINT_SCENARIO_CONFIG) args.push("-c", process.env.LINT_SCENARIO_CONFIG);
      const result = spawnSync("pnpm", [...args, path], { encoding: "utf8", timeout: 30000 });
      assert.ifError(result.error);
      const output = result.stdout + result.stderr;
      if (rule) {
        assert.notEqual(result.status, 0, `Expected ${rule} rejection`);
        assert.ok(output.includes(rule), output);
      } else assert.equal(result.status, 0, output);
    } finally {
      unlinkSync(path);
    }
  });
}
