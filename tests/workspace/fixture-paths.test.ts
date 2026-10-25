import assert from "node:assert/strict";
import { lstatSync, realpathSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

import { installBaseline, runCommand, withWorkspaceFixture, workspaceRoot } from "#workspace/workspace-fixture";

test("owned fixture paths preserve member and virtual-store ownership through package links", async () => {
  await withWorkspaceFixture(async (context) => {
    await installBaseline(context);
    const temporaryRoot = join(context.directory, "node_modules/.pnpm/node_modules/portcullis-web");
    assert.ok(lstatSync(temporaryRoot).isSymbolicLink(), "The ordinary installation must supply the package link");
    const physicalTemporaryRoot = realpathSync(temporaryRoot);
    assert.notEqual(temporaryRoot, physicalTemporaryRoot, "The temporary ancestor must expose distinct logical and physical paths");
    assert.equal(physicalTemporaryRoot, join(context.directory, "web"), "The package link must target the owned member outside installed dependencies");
    context.environment = { ...context.environment, TMPDIR: temporaryRoot, TMP: temporaryRoot, TEMP: temporaryRoot };
    delete context.environment.NODE_TEST_CONTEXT;
    const scenarios = [
      { file: "non-interactive-install.test.ts", name: "piped installation completes a frozen virtual-store change without requiring a terminal" },
      { file: "package-workspace.test.ts", name: "member commands retain their package ownership and shared compiler tools" },
    ];
    const result = await runCommand(context, process.execPath, [
      "--test",
      "--test-reporter=tap",
      `--test-name-pattern=^(?:${scenarios.map(({ name }) => name).join("|")})$`,
      ...scenarios.map(({ file }) => join(workspaceRoot, "tests/workspace", file)),
    ]);
    assert.equal(result.status, 0, result.output);
    assert.match(result.output, /^# tests 2$/m, "Both actual ownership scenarios must execute");
    assert.match(result.output, /^# pass 2$/m, result.output);
    assert.match(result.output, /^# fail 0$/m, result.output);
    assert.match(result.output, /^# skipped 0$/m, "A name-filter mismatch must not bypass either scenario");
  });
});
