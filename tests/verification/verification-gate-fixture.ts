import assert from "node:assert/strict";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, sep } from "node:path";
import { fileURLToPath } from "node:url";

import { executeOwnedVerification, type VerificationContext, type VerificationResult } from "#verification/verification-process";

const projectRoot = fileURLToPath(new URL("../..", import.meta.url));
const workspaceInputs = ["package.json", "pnpm-workspace.yaml", "pnpm-lock.yaml", "web/package.json", "tests/load/package.json"];
const verificationConfiguration = ["tests/verification/tsconfig.json", "tests/verification/oxlint.config.ts"];
type ToolDependency = "typescript" | "@types/node" | "oxlint";
type FixtureLocation = "direct" | "aliased";
type GateFailure = { kind: "none" } | { kind: "failed"; error: unknown };
type WorkspaceSnapshot = { kind: "not-copied" } | { kind: "copied"; contents: Buffer[] };

/** Supplies the owned launch witness alongside the normal process context. */
export type VerificationGateFixture = VerificationContext & { marker: string };

/** Copies one actual target and its recipe, retaining its prerequisite declaration. */
function readMakeTarget(target: "workspace-install" | "test-scheduling-check"): string {
  const lines = readFileSync(join(projectRoot, "Makefile"), "utf8").split("\n");
  const positions = lines.flatMap((line, index) => line.startsWith(`${target}:`) ? [index] : []);
  const [position] = positions;
  assert.equal(positions.length, 1, `Exactly one ${target} target must exist`);
  assert.ok(position !== undefined);
  const declaration = lines[position];
  assert.ok(declaration !== undefined);
  const recipe: string[] = [];
  for (const line of lines.slice(position + 1)) {
    if (!line.startsWith("\t")) break;
    recipe.push(line);
  }
  assert.ok(recipe.length > 0, `${target} must have an executable recipe`);
  return `${declaration}\n${recipe.join("\n")}\n`;
}

/** Installs declared root tools in a fresh fixture before qualifying its canonical gate. */
export async function withVerificationGateFixture(scenario: (context: VerificationGateFixture) => Promise<void>, location: FixtureLocation = "direct"): Promise<void> {
  const ownedBase = mkdtempSync(join(tmpdir(), "portcullis-verification-gate-"));
  const physicalDirectory = join(ownedBase, "project");
  const directory = location === "direct" ? physicalDirectory : join(ownedBase, "workspace-alias");
  const marker = join(directory, "runtime-launch-marker");
  const environment: NodeJS.ProcessEnv = { ...process.env, PORTCULLIS_GATE_MARKER: marker };
  delete environment.NODE_TEST_CONTEXT;
  const context: VerificationGateFixture = { directory, marker, environment, processState: { kind: "idle" } };
  let failure: GateFailure = { kind: "none" };
  let snapshot: WorkspaceSnapshot = { kind: "not-copied" };
  try {
    mkdirSync(physicalDirectory);
    if (location === "aliased") symlinkSync(physicalDirectory, directory, "dir");
    for (const source of [...workspaceInputs, ...verificationConfiguration]) {
      const destination = join(directory, source);
      mkdirSync(dirname(destination), { recursive: true });
      copyFileSync(join(projectRoot, source), destination);
    }
    writeFileSync(join(directory, "Makefile"), `.PHONY: workspace-install test-scheduling-check\n${readMakeTarget("workspace-install")}\n${readMakeTarget("test-scheduling-check")}`);
    snapshot = { kind: "copied", contents: workspaceInputs.map((source) => readFileSync(join(directory, source))) };
    const installation = await executeOwnedVerification(context, "make", ["workspace-install"]);
    assert.equal(installation.status, 0, `A real fresh frozen root install must work before gate admission:\n${installation.output}`);
    await scenario(context);
  } catch (error: unknown) {
    failure = { kind: "failed", error };
  }
  try {
    if (snapshot.kind === "copied") {
      assert.deepEqual(workspaceInputs.map((source) => readFileSync(join(directory, source))), snapshot.contents, "Gate qualification must preserve pinned workspace inputs");
    }
  } catch (error: unknown) {
    failure = { kind: "failed", error: failure.kind === "failed" ? new AggregateError([failure.error, error], "Gate execution and pinned-input validation both failed") : error };
  }
  try {
    assert.equal(context.processState.kind, "idle", `Owned processes remain active; gate fixture retained at ${ownedBase}`);
    rmSync(ownedBase, { recursive: true, force: true });
    assert.equal(existsSync(ownedBase), false, "The physical gate fixture and its alias must be removed");
  } catch (error: unknown) {
    if (failure.kind === "failed") throw new AggregateError([failure.error, error], "Gate execution and disposal both failed");
    throw error;
  }
  if (failure.kind === "failed") throw failure.error;
}

/** Writes runnable erasable TypeScript with a witness at the execution boundary. */
export function writeGateSource(context: VerificationGateFixture, body: string, imports = ""): void {
  writeFileSync(join(context.directory, "tests/verification/gate.test.ts"), `import assert from "node:assert/strict";
import { writeFileSync } from "node:fs";
import test from "node:test";
${imports}

test("canonical gate execution witness", () => {
  const marker = process.env.PORTCULLIS_GATE_MARKER;
  if (typeof marker !== "string") throw new Error("The owned gate marker must be configured");
  assert.notEqual(marker, "");
  ${body}
  writeFileSync(marker, "executed");
});
`);
}

/** Writes one typed helper to exercise real Node package-subpath resolution. */
export function writeGateValueModule(context: VerificationGateFixture): void {
  writeFileSync(join(context.directory, "tests/verification/gate-value.ts"), `/** Defines a value supplied through the repository alias. */
export type GateValue = { kind: "ready"; value: number };
export const gateValue: GateValue = { kind: "ready", value: 7 };
`);
}

/** Runs the copied actual Make gate without inheriting an earlier launch witness. */
export async function runCanonicalGate(context: VerificationGateFixture): Promise<VerificationResult> {
  rmSync(context.marker, { force: true });
  return executeOwnedVerification(context, "make", ["test-scheduling-check"]);
}

/** Runs the same source directly to prove it is executable before static admission. */
export async function runNativeGateSource(context: VerificationGateFixture): Promise<VerificationResult> {
  rmSync(context.marker, { force: true });
  return executeOwnedVerification(context, process.execPath, ["--test", "tests/verification/gate.test.ts"]);
}

/** Requires successful execution of the fixture's actual test body. */
export function assertGateLaunched(context: VerificationGateFixture, result: VerificationResult): void {
  assert.equal(result.status, 0, result.output);
  assert.equal(existsSync(context.marker), true, `A successful canonical command must execute the fixture body:\n${result.output}`);
  assert.equal(readFileSync(context.marker, "utf8"), "executed");
}

/** Refuses the intended static category before any fixture test body can launch. */
export function assertGateRefused(context: VerificationGateFixture, result: VerificationResult, category: RegExp): void {
  assert.notEqual(result.status, 0, `The checked gate must reject runnable invalid source; launch witness exists: ${existsSync(context.marker)}\n${result.output}`);
  assert.match(result.output, category, result.output);
  assert.equal(existsSync(context.marker), false, "Static refusal must precede test-body execution");
}

/** Reads a declared package version through an explicit JSON boundary. */
function readInstalledVersion(directory: string, dependency: ToolDependency): string {
  const parsed: unknown = JSON.parse(readFileSync(join(directory, "node_modules", dependency, "package.json"), "utf8"));
  assert.ok(typeof parsed === "object" && parsed !== null && "version" in parsed);
  const { version } = parsed;
  assert.equal(typeof version, "string");
  assert.ok(typeof version === "string" && version !== "");
  return version;
}

/** Verifies fixture-owned tools resolve the same pinned versions as the root consumer. */
export async function assertDeclaredToolsOwned(context: VerificationGateFixture): Promise<void> {
  const expectedModules = join(realpathSync(context.directory), "node_modules");
  const dependencies: readonly ToolDependency[] = ["typescript", "@types/node", "oxlint"];
  for (const dependency of dependencies) {
    const installed = join(context.directory, "node_modules", dependency);
    assert.equal(existsSync(join(installed, "package.json")), true, `${dependency} must be installed for its declared root consumer`);
    assert.ok(realpathSync(installed).startsWith(`${expectedModules}${sep}`), "Tool resolution must remain in the fresh fixture modules");
    assert.equal(readInstalledVersion(context.directory, dependency), readInstalledVersion(projectRoot, dependency));
  }
  for (const tool of ["tsc", "oxlint"]) {
    const result = await executeOwnedVerification(context, "pnpm", ["exec", tool, "--version"]);
    assert.equal(result.status, 0, result.output);
    assert.notEqual(result.output.trim(), "");
  }
}

/** Repoints one fixture dependency to the known pinned project tool for refusal qualification. */
export function linkDependencyToProjectTool(context: VerificationGateFixture): void {
  assert.equal(context.processState.kind, "idle", "A fixture must be idle before its installed links change");
  const installed = join(context.directory, "node_modules", "typescript");
  rmSync(installed, { recursive: true, force: true });
  symlinkSync(join(projectRoot, "node_modules", "typescript"), installed, "dir");
}

/** Repoints only the fixture's modules entry to known project modules for refusal qualification. */
export function linkModulesToProjectTools(context: VerificationGateFixture): void {
  assert.equal(context.processState.kind, "idle", "A fixture must be idle before its installed links change");
  const installed = join(context.directory, "node_modules");
  rmSync(installed, { recursive: true, force: true });
  symlinkSync(join(projectRoot, "node_modules"), installed, "dir");
}
