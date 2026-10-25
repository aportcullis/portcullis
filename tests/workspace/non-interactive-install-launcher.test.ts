import assert from "node:assert/strict";
import { chmodSync, copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { delimiter, join } from "node:path";
import test from "node:test";

import { assertInstallAccepted, installThroughPipe, withInstallFixture } from "#workspace/non-interactive-install-fixture";
import { workspaceRoot } from "#workspace/workspace-fixture";

test("the actual Make recipe retains the admitted package manager when PATH changes", async () => {
  await withInstallFixture((context) => {
    const shadowDirectory = join(context.directory, "path-shadow");
    mkdirSync(shadowDirectory);
    const shadowExecutable = join(shadowDirectory, "pnpm");
    copyFileSync(join(workspaceRoot, "tests/workspace/non-interactive-install-path-probe.sh"), shadowExecutable);
    chmodSync(shadowExecutable, 0o755);
    const { PATH } = context.environment;
    assert.ok(typeof PATH === "string");
    context.environment.PATH = `${shadowDirectory}${delimiter}${PATH}`;
    const makefile = join(context.directory, "Makefile");
    const actualRecipe = readFileSync(makefile, "utf8");
    const admittedCommand = 'CI=true "$(PNPM)" install --frozen-lockfile';
    assert.ok(actualRecipe.includes(admittedCommand), "The copied recipe must select its package manager explicitly");
    try {
      writeFileSync(makefile, actualRecipe.replace(admittedCommand, "CI=true pnpm install --frozen-lockfile"));
      const refused = installThroughPipe(context);
      assert.notEqual(refused.status, 0, "Searching PATH again must encounter the refused launcher");
      assert.match(refused.output, /Unexpected PATH package manager/, `The old recipe must actually execute the PATH launcher:\n${refused.output}`);
      assert.equal(existsSync(join(context.directory, "verification-witness.json")), false, "A refused installer must stop downstream verification");
    } finally {
      writeFileSync(makefile, actualRecipe);
    }
    const accepted = installThroughPipe(context);
    assertInstallAccepted(context, accepted);
    assert.doesNotMatch(accepted.output, /Unexpected PATH package manager/, "The actual recipe must retain the admitted launcher");
    assert.equal(readFileSync(makefile, "utf8"), actualRecipe, "The owned recipe must be restored exactly");
  });
});
