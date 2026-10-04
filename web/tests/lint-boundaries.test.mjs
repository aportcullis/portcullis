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
  ["as type assertions are rejected", "features/request", 'export const value = JSON.parse("1") as number;', "consistent-type-assertions"],
  ["angle-bracket type assertions are rejected", "entities/request", 'export const value = <number>JSON.parse("1");', "consistent-type-assertions"],
  ["non-null assertions are rejected", "shared", "export const value = [1].find(Boolean)!;", "no-non-null-assertion"],
  ["non-null assertions on members are rejected", "app", "export const value = document.body.querySelector('main')!.id;", "no-non-null-assertion"],
  ["const assertions remain allowed", "shared", 'export const value = ["read", "write"] as const;', null],
  ["satisfies remains allowed", "shared", "export const value = { count: 1 } satisfies { count: number };", null],
  ["type annotations remain allowed", "shared", 'export const value: number = JSON.parse("1");', null],
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
