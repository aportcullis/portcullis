import assert from "node:assert/strict";
import { existsSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

import { installThroughPipe, packageName, withInstallFixture } from "#workspace/non-interactive-install-fixture";

const refusedConfigurations: readonly { name: string; value: unknown; category: RegExp }[] = [
  { name: "non-object configuration", value: [], category: /Fixture configuration must be an object/ },
  { name: "missing package identity", value: { members: ["web", "tests/load"], witnessFile: "verification-witness.json" }, category: /Fixture package name must be/ },
  { name: "escaping member directory", value: { packageName, members: ["../outside"], witnessFile: "verification-witness.json" }, category: /Fixture members must be/ },
  { name: "escaping witness output", value: { packageName, members: ["web", "tests/load"], witnessFile: "../outside.json" }, category: /Fixture witness file must be/ },
];

for (const { name, value, category } of refusedConfigurations) {
  test(`the static install witness refuses a ${name}`, async () => {
    await withInstallFixture((context) => {
      writeFileSync(join(context.directory, "verification-config.json"), JSON.stringify(value));
      const result = installThroughPipe(context);
      assert.notEqual(result.status, 0, "Invalid witness configuration must refuse downstream verification");
      assert.match(result.output, category, result.output);
      assert.equal(existsSync(join(context.directory, "verification-witness.json")), false, "Refusal must not create the execution witness");
      assert.equal(existsSync(context.marker), false, "Refusal must not enable a dependency script");
    });
  });
}
