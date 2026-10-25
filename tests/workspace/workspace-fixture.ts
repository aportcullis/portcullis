import assert from "node:assert/strict";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join } from "node:path";
import { fileURLToPath } from "node:url";

import { executeOwnedVerification, type VerificationContext, type VerificationResult } from "#verification/verification-process";

export const workspaceRoot = fileURLToPath(new URL("../..", import.meta.url));
export const syntheticName = "portcullis-synthetic-build";
export const members: readonly { directory: string; name: string; scripts: readonly string[] }[] = [
  { directory: "web", name: "portcullis-web", scripts: ["build", "typecheck", "lint", "test", "e2e"] },
  { directory: "tests/load", name: "portcullis-load-tests", scripts: ["bundle", "typecheck", "lint", "test"] },
];
type Failure = { kind: "none" } | { kind: "failed"; error: unknown };
type JsonRecord = Record<string, unknown>;

/** Records one workspace's owned commands, selected store and frozen-install phase. */
export type WorkspaceFixture = VerificationContext & {
  marker: string;
  store: { kind: "unresolved" } | { kind: "resolved"; path: string };
  baseline: { kind: "cold" } | { kind: "seeded" };
};

/** Rejects non-object JSON before callers inspect or mutate package fields. */
export function requireJsonRecord(value: unknown): asserts value is JsonRecord {
  assert.ok(typeof value === "object" && value !== null && !Array.isArray(value), "Package data must be a JSON object");
}

/** Reads package JSON through an explicit unknown boundary. */
export function readPackageManifest(path: string): JsonRecord {
  const parsed: unknown = JSON.parse(readFileSync(path, "utf8"));
  requireJsonRecord(parsed);
  return parsed;
}

/** Runs an owned command while retaining the admitted package manager. */
export async function runCommand(context: VerificationContext, command: string, arguments_: readonly string[]): Promise<VerificationResult> {
  const executable = command === "pnpm" ? requireActivePackageManager(context.environment) : command;
  const result = await executeOwnedVerification(context, executable, arguments_);
  assert.equal(result.signal, null, `Command was terminated: ${command}`);
  return result;
}

/** Resolves the configured store from the canonical root while retaining caller overrides. */
export async function resolveWorkspaceStore(context: VerificationContext, root: string = workspaceRoot): Promise<string> {
  const result = await runCommand(context, "pnpm", ["-C", root, "store", "path"]);
  assert.equal(result.status, 0, result.output);
  const store = result.output.trim();
  assert.ok(store !== "" && isAbsolute(store) && !/[\r\n]/.test(store), "pnpm must report one absolute configured store path");
  return store;
}

/** Retains the declared package manager that admitted the current test script. */
export function requireActivePackageManager(environment: NodeJS.ProcessEnv): string {
  const { npm_execpath: executable, npm_config_user_agent: userAgent } = environment;
  assert.ok(typeof executable === "string" && isAbsolute(executable), "The active pnpm script launcher must be an absolute path; run the checked workspace script");
  const { packageManager } = readPackageManifest(join(workspaceRoot, "package.json"));
  assert.ok(typeof packageManager === "string" && /^pnpm@\d+\.\d+\.\d+$/.test(packageManager), "The root must declare an exact pnpm version");
  const expectedAgent = `pnpm/${packageManager.slice("pnpm@".length)}`;
  assert.ok(typeof userAgent === "string" && userAgent.split(" ")[0] === expectedAgent, "The active package manager must match the declared pnpm version");
  return executable;
}

/** Copies real frozen inputs without inheriting installed modules or losing command ownership. */
export async function withWorkspaceFixture(scenario: (context: WorkspaceFixture) => Promise<void>, storeMode: "canonical" | "owned-empty" = "canonical"): Promise<void> {
  const directory = realpathSync(mkdtempSync(join(tmpdir(), "portcullis-workspace-")));
  const marker = join(directory, "unreviewed-script-ran");
  const environment: NodeJS.ProcessEnv = { ...process.env, CI: "true", PORTCULLIS_SCRIPT_MARKER: marker };
  if (storeMode === "owned-empty") {
    environment.npm_config_store_dir = join(directory, "owned-store");
    environment.NPM_CONFIG_STORE_DIR = join(directory, "owned-store");
  }
  const context: WorkspaceFixture = { directory, environment, marker, store: { kind: "unresolved" }, baseline: { kind: "cold" }, processState: { kind: "idle" } };
  let failure: Failure = { kind: "none" };
  try {
    const sources = ["package.json", "pnpm-workspace.yaml", "pnpm-lock.yaml", ...members.map(({ directory: memberDirectory }) => `${memberDirectory}/package.json`)];
    for (const source of sources) {
      const destination = join(directory, source);
      mkdirSync(dirname(destination), { recursive: true });
      copyFileSync(join(workspaceRoot, source), destination);
    }
    context.store = { kind: "resolved", path: await resolveWorkspaceStore(context) };
    await scenario(context);
  } catch (error: unknown) {
    failure = { kind: "failed", error };
  }
  try {
    assert.equal(context.processState.kind, "idle", `Owned processes remain active; workspace fixture retained at ${directory}`);
    await rm(directory, { recursive: true, force: true });
    assert.equal(existsSync(directory), false, "The owned workspace fixture must be removed");
  } catch (error: unknown) {
    if (failure.kind === "failed") throw new AggregateError([failure.error, error], "Workspace scenario and disposal both failed");
    throw error;
  }
  if (failure.kind === "failed") throw failure.error;
}

/** Installs from the selected store with ordinary frozen/script-policy enforcement. */
export async function install(context: WorkspaceFixture, additionalArguments: readonly string[] = ["--frozen-lockfile"], mode: "offline" | "normal" = "offline"): Promise<VerificationResult> {
  assert.ok(context.store.kind === "resolved", "The configured store must be resolved before installation");
  const networkArguments = mode === "offline" ? ["--offline"] : [];
  const result = await runCommand(context, "pnpm", ["install", ...networkArguments, `--store-dir=${context.store.path}`, ...additionalArguments]);
  assert.doesNotMatch(result.output, /ERR_PNPM_(?:NO_OFFLINE_META|NO_OFFLINE_TARBALL|FETCH_[A-Z_]+)/, `Cache or dependency-fetch failure is not a policy refusal:\n${result.output}`);
  return result;
}

/** Establishes a usable untouched installation before any policy refusal. */
export async function installBaseline(context: WorkspaceFixture): Promise<void> {
  const mode = context.baseline.kind === "cold" ? "normal" : "offline";
  const result = await install(context, ["--frozen-lockfile"], mode);
  assert.equal(result.status, 0, `Fresh root frozen install must succeed:\n${result.output}`);
  assert.equal(existsSync(context.marker), false, "No installation script may create a marker");
  context.baseline = { kind: "seeded" };
}

/** Removes only fixture modules so subsequent installs exercise fresh enforcement. */
export function clearInstalledModules(context: WorkspaceFixture): void {
  assert.equal(context.processState.kind, "idle");
  for (const directory of [context.directory, ...members.map(({ directory: memberDirectory }) => join(context.directory, memberDirectory))]) {
    rmSync(join(directory, "node_modules"), { recursive: true, force: true });
  }
}

/** Requires the intended frozen refusal with unchanged lock bytes and no script marker. */
export async function assertInstallRefused(context: WorkspaceFixture, category: string, lockBefore: Buffer): Promise<VerificationResult> {
  clearInstalledModules(context);
  const result = await install(context);
  assert.notEqual(result.status, 0, "A frozen install must reject the changed input");
  assert.match(result.output, new RegExp(`\\b${category}\\b`), result.output);
  assert.equal(existsSync(context.marker), false, "A refused install must not execute dependency scripts");
  assert.deepEqual(readFileSync(join(context.directory, "pnpm-lock.yaml")), lockBefore, "Refusal must leave the root lock unchanged");
  return result;
}

/** Adds one reviewed local package without enabling its dependency build script. */
export async function addReviewedSyntheticPackage(context: WorkspaceFixture): Promise<void> {
  const packageDirectory = join(context.directory, "synthetic/package");
  mkdirSync(packageDirectory, { recursive: true });
  const postinstall = join(context.directory, "fixture-postinstall.ts");
  copyFileSync(join(workspaceRoot, "tests/workspace/non-interactive-install-postinstall.ts"), postinstall);
  context.environment.PORTCULLIS_FIXTURE_NODE = process.execPath;
  context.environment.PORTCULLIS_FIXTURE_POSTINSTALL = postinstall;
  writeFileSync(join(packageDirectory, "package.json"), JSON.stringify({
    name: syntheticName, version: "1.0.0", main: "index.json",
    scripts: { postinstall: '"$PORTCULLIS_FIXTURE_NODE" "$PORTCULLIS_FIXTURE_POSTINSTALL"' },
  }));
  writeFileSync(join(packageDirectory, "index.json"), JSON.stringify({ usable: "reviewed-without-build" }));
  const archive = await runCommand(context, "tar", ["-czf", join(context.directory, "synthetic.tgz"), "-C", join(context.directory, "synthetic"), "package"]);
  assert.equal(archive.status, 0, archive.output);
  const manifestPath = join(context.directory, "package.json");
  const manifest = readPackageManifest(manifestPath);
  const dependencies = manifest.dependencies;
  if (dependencies === undefined) manifest.dependencies = { [syntheticName]: "file:synthetic.tgz" };
  else { requireJsonRecord(dependencies); dependencies[syntheticName] = "file:synthetic.tgz"; }
  writeFileSync(manifestPath, JSON.stringify(manifest));
  const policyPath = join(context.directory, "pnpm-workspace.yaml");
  const policy = readFileSync(policyPath, "utf8");
  assert.match(policy, /^allowBuilds:\s*$/m, "The real root policy must declare reviewed build decisions");
  writeFileSync(policyPath, policy.replace(/^allowBuilds:\s*$/m, `allowBuilds:\n  ${syntheticName}: false`));
  const resolution = await install(context, ["--lockfile-only", "--no-frozen-lockfile"]);
  assert.equal(resolution.status, 0, resolution.output);
  clearInstalledModules(context);
  await installBaseline(context);
}
