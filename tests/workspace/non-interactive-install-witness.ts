import assert from "node:assert/strict";
import { writeFileSync } from "node:fs";
import { createRequire } from "node:module";

import { isFixtureRecord, readInstallWitnessConfiguration } from "#workspace/non-interactive-install-config";

/** Verifies the installed members and records the caller's unchanged CI state. */
function runInstallWitness(): void {
  const { packageName, members, witnessFile } = readInstallWitnessConfiguration("verification-config.json");
  for (const member of members) {
    const require = createRequire(new URL(`./${member}/package.json`, import.meta.url));
    const installed: unknown = require(packageName);
    assert.ok(isFixtureRecord(installed));
    assert.equal(installed.usable, "local-without-build");
  }
  const { CI } = process.env;
  const callerCI = CI === undefined ? { kind: "unset" } : { kind: "set", value: CI };
  writeFileSync(witnessFile, JSON.stringify(callerCI));
}

runInstallWitness();
