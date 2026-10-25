import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { writeFileSync } from "node:fs";

import { readInstallHookConfiguration } from "#workspace/non-interactive-install-config";

/** Runs the configured real hook with piped reference input and native status reporting. */
function runInstallHook(): void {
  const { executable, referenceInput, processGroupFile, statusFile } = readInstallHookConfiguration("hook-config.json");
  writeFileSync(processGroupFile, String(process.pid));
  const result = spawnSync(executable, ["run", "pre-push"], { stdio: ["pipe", "inherit", "inherit"], input: referenceInput });
  assert.ifError(result.error);
  const { status, signal } = result;
  writeFileSync(statusFile, JSON.stringify({ status, signal }));
  process.exit(typeof status === "number" ? status : 1);
}

runInstallHook();
