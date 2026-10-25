import assert from "node:assert/strict";
import { copyFileSync, realpathSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import type { VerificationResult } from "#verification/verification-process";
import { members, runCommand, workspaceRoot, type WorkspaceFixture } from "#workspace/workspace-fixture";

/** Supplies the operations accepted by the checked member-owned probe. */
export type WorkspaceProbeConfiguration =
  | { kind: "module" }
  | { kind: "member"; name: string; directory: string; scripts: readonly string[] }
  | { kind: "synthetic"; packageName: string };

/** Copies a checked program to an owned member and passes configuration only as data. */
export async function runWorkspaceProbe(context: WorkspaceFixture, member: { name: string; directory: string }, configuration: WorkspaceProbeConfiguration): Promise<VerificationResult> {
  assert.equal(context.processState.kind, "idle");
  assert.ok(members.some(({ name, directory }) => name === member.name && directory === member.directory), "The probe must use a declared workspace member");
  const directory = realpathSync(join(context.directory, member.directory));
  assert.equal(directory, join(realpathSync(context.directory), member.directory), "The probe program must remain in its owned physical member");
  const program = join(directory, "workspace-probe.ts");
  const configurationPath = join(directory, "workspace-probe.json");
  copyFileSync(join(workspaceRoot, "tests/load/workspace-probe.ts"), program);
  writeFileSync(configurationPath, JSON.stringify(configuration));
  if (configuration.kind === "synthetic") return runCommand(context, process.execPath, [program, configurationPath]);
  return runCommand(context, "pnpm", ["--filter", member.name, "exec", process.execPath, program, configurationPath]);
}
