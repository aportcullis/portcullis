import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { readPackageManifest, requireActivePackageManager } from "#workspace/workspace-fixture";

const projectRoot = fileURLToPath(new URL("../..", import.meta.url));
export const packageName = "portcullis-install-fixture";
export const memberDirectories = ["web", "tests/load"];
const ownershipPaths = ["package.json", "pnpm-workspace.yaml", "pnpm-lock.yaml", ...memberDirectories.map((member) => `${member}/package.json`)];
export const purgePrompt = /will be removed and reinstalled from scratch\. Proceed\?/;

/** Retains the isolated install inputs and script witness for one scenario. */
export type InstallFixture = { directory: string; environment: NodeJS.ProcessEnv; marker: string };

/** Preserves a command's native status and captured diagnostics. */
export type InstallCommandResult = { status: number | null; stdout: string; output: string };

/** Records the exact bytes or absence of one frozen installation input. */
export type InstallOwnershipInput = { path: string; kind: "present"; bytes: Buffer } | { path: string; kind: "absent" };

/** Records whether the caller supplied CI to downstream verification. */
type CallerCI = { kind: "unset" } | { kind: "set"; value: string };

/** Extracts the actual install recipe without running Make's hook setup. */
function readWorkspaceInstallRecipe(): string {
  const lines = readFileSync(join(projectRoot, "Makefile"), "utf8").split("\n");
  const targets = lines.flatMap((line, index) => line === "workspace-install:" ? [index] : []);
  const [target] = targets;
  assert.equal(targets.length, 1, "Exactly one workspace-install target must exist");
  assert.ok(target !== undefined);
  const recipe: string[] = [];
  for (const line of lines.slice(target + 1)) {
    if (!line.startsWith("\t")) break;
    recipe.push(line.slice(1));
  }
  const [command] = recipe;
  assert.equal(recipe.length, 1, "The shared installation must have one nonempty recipe");
  assert.ok(command !== undefined && command.trim() !== "");
  return command;
}

/** Runs a bounded real command and retains its complete diagnostics. */
export function runInstallCommand(command: string, arguments_: readonly string[], directory: string, environment: NodeJS.ProcessEnv): InstallCommandResult {
  const executable = command === "pnpm" ? requireActivePackageManager(environment) : command;
  const result = spawnSync(executable, arguments_, { cwd: directory, env: environment, encoding: "utf8", timeout: 30000 });
  assert.ifError(result.error);
  assert.equal(result.signal, null, `${command} was terminated`);
  return { status: result.status, stdout: result.stdout, output: result.stdout + result.stderr };
}

/** Builds a local-only workspace with its own configuration, tarball and stores. */
function createInstallFixture(directory: string): InstallFixture {
  for (const path of ["web", "tests/load", "synthetic/package", "store-a", "store-b", "config", "tmp", "hooks", "templates"]) {
    mkdirSync(join(directory, path), { recursive: true });
  }
  for (const name of ["npmrc", "gitconfig", "attributes", "excludes"]) writeFileSync(join(directory, "config", name), "");
  const activePackageManager = requireActivePackageManager(process.env);
  const { PATH, COREPACK_HOME, npm_config_user_agent: userAgent } = process.env;
  assert.ok(typeof PATH === "string", "The pinned package manager must be on PATH");
  assert.ok(typeof userAgent === "string");
  const marker = join(directory, "script-marker");
  const environment: NodeJS.ProcessEnv = {
    PATH,
    PNPM: activePackageManager,
    npm_execpath: activePackageManager,
    npm_config_user_agent: userAgent,
    COREPACK_ENABLE_NETWORK: "0",
    NPM_CONFIG_USERCONFIG: join(directory, "config/npmrc"),
    NPM_CONFIG_GLOBALCONFIG: join(directory, "config/npmrc"),
    NPM_CONFIG_CACHE: join(directory, "config/npm-cache"),
    npm_config_offline: "true",
    npm_config_store_dir: join(directory, "store-a"),
    XDG_CONFIG_HOME: join(directory, "config/xdg"),
    XDG_CACHE_HOME: join(directory, "config/cache"),
    XDG_DATA_HOME: join(directory, "config/data"),
    TMPDIR: join(directory, "tmp"),
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_CONFIG_GLOBAL: join(directory, "config/gitconfig"),
    GIT_ATTR_NOSYSTEM: "1",
    GIT_TERMINAL_PROMPT: "0",
    GIT_TEMPLATE_DIR: join(directory, "templates"),
    PORTCULLIS_SCRIPT_MARKER: marker,
    PORTCULLIS_FIXTURE_NODE: process.execPath,
    PORTCULLIS_FIXTURE_POSTINSTALL: join(directory, "postinstall.ts"),
    TERM: "dumb",
    NO_COLOR: "1",
  };
  if (typeof COREPACK_HOME === "string") environment.COREPACK_HOME = COREPACK_HOME;
  const { packageManager } = readPackageManifest(join(projectRoot, "package.json"));
  assert.ok(typeof packageManager === "string");
  writeFileSync(join(directory, "package.json"), JSON.stringify({
    name: "portcullis-workspace", private: true, packageManager, type: "module",
    imports: { "#workspace/non-interactive-install-config": "./fixture-config.ts" },
  }));
  for (const [index, member] of memberDirectories.entries()) {
    writeFileSync(join(directory, member, "package.json"), JSON.stringify({
      name: index === 0 ? "portcullis-web" : "portcullis-load-tests",
      private: true,
      dependencies: { [packageName]: `file:${index === 0 ? ".." : "../.."}/synthetic.tgz` },
    }));
  }
  const policy = readFileSync(join(projectRoot, "pnpm-workspace.yaml"), "utf8");
  assert.match(policy, /^allowBuilds:\s*$/m);
  writeFileSync(join(directory, "pnpm-workspace.yaml"), policy.replace(/^allowBuilds:\s*$/m, `allowBuilds:\n  ${packageName}: false`));
  writeFileSync(join(directory, "synthetic/package/package.json"), JSON.stringify({
    name: packageName,
    version: "1.0.0",
    main: "index.json",
    scripts: { postinstall: '"$PORTCULLIS_FIXTURE_NODE" "$PORTCULLIS_FIXTURE_POSTINSTALL"' },
  }));
  writeFileSync(join(directory, "synthetic/package/index.json"), JSON.stringify({ usable: "local-without-build" }));
  const archive = runInstallCommand("tar", ["-czf", "synthetic.tgz", "-C", "synthetic", "package"], directory, environment);
  assert.equal(archive.status, 0, archive.output);
  const resolution = runInstallCommand("pnpm", ["install", "--lockfile-only", "--no-frozen-lockfile"], directory, { ...environment, CI: "true" });
  assert.equal(resolution.status, 0, resolution.output);
  for (const [source, destination] of [
    ["non-interactive-install-config.ts", "fixture-config.ts"],
    ["non-interactive-install-witness.ts", "witness.ts"],
    ["non-interactive-install-hook.ts", "hook-wrapper.ts"],
    ["non-interactive-install-postinstall.ts", "postinstall.ts"],
  ]) {
    assert.ok(source !== undefined && destination !== undefined);
    copyFileSync(join(projectRoot, "tests/workspace", source), join(directory, destination));
  }
  writeFileSync(join(directory, "verification-config.json"), JSON.stringify({ packageName, members: memberDirectories, witnessFile: "verification-witness.json" }));
  writeFileSync(join(directory, "Makefile"), `.PHONY: verify workspace-install\nworkspace-install:\n\t${readWorkspaceInstallRecipe()}\nverify: workspace-install\n\t@"$$PORTCULLIS_FIXTURE_NODE" witness.ts\n`);
  writeFileSync(join(directory, "lefthook.yml"), "pre-push:\n  follow: true\n  commands:\n    verify:\n      run: 'echo $$ > hook-process-group; exec make verify'\n");
  const initialized = runInstallCommand("git", ["-c", `core.hooksPath=${join(directory, "hooks")}`, "-c", `core.attributesFile=${join(directory, "config/attributes")}`, "-c", `core.excludesFile=${join(directory, "config/excludes")}`, "init", "--quiet", `--template=${join(directory, "templates")}`], directory, environment);
  assert.equal(initialized.status, 0, initialized.output);
  const staged = runInstallCommand("git", ["-c", `core.attributesFile=${join(directory, "config/attributes")}`, "add", "Makefile", "lefthook.yml", "witness.ts", "fixture-config.ts", "hook-wrapper.ts", "postinstall.ts", "verification-config.json", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "web/package.json", "tests/load/package.json", "synthetic.tgz"], directory, environment);
  assert.equal(staged.status, 0, staged.output);
  const committed = runInstallCommand("git", ["-c", `core.hooksPath=${join(directory, "hooks")}`, "-c", "commit.gpgsign=false", "-c", "user.name=Install fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "feat: prepare isolated install fixture"], directory, environment);
  assert.equal(committed.status, 0, committed.output);
  return { directory, environment, marker };
}

/** Removes only this invocation's fixture and verifies that cleanup completed. */
export async function withInstallFixture(scenario: (context: InstallFixture) => void | Promise<void>): Promise<void> {
  const directory = realpathSync(mkdtempSync(join(tmpdir(), "portcullis-noninteractive-install-")));
  try {
    const context = createInstallFixture(directory);
    await scenario(context);
  } finally {
    await rm(directory, { recursive: true, force: true });
    assert.equal(existsSync(directory), false, "Owned fixture must be removed");
  }
}

/** Executes the actual extracted recipe before the downstream CI witness. */
export function installThroughPipe(context: InstallFixture, changes: NodeJS.ProcessEnv = {}): InstallCommandResult {
  const inputs = readInstallOwnershipInputs(context);
  rmSync(join(context.directory, "verification-witness.json"), { force: true });
  const result = runInstallCommand("make", ["verify"], context.directory, { ...context.environment, ...changes });
  assert.deepEqual(readInstallOwnershipInputs(context), inputs, "Installation must preserve lockfile, policy and member manifests");
  return result;
}

/** Snapshots present and absent ownership inputs without normalizing their bytes. */
export function readInstallOwnershipInputs(context: InstallFixture): InstallOwnershipInput[] {
  return ownershipPaths.map((path): InstallOwnershipInput => {
    const input = join(context.directory, path);
    return existsSync(input) ? { path, kind: "present", bytes: readFileSync(input) } : { path, kind: "absent" };
  });
}

/** Refuses the intended policy category before downstream verification or scripts. */
export function assertInstallRefused(context: InstallFixture, result: { status: number | null; output: string }, category: RegExp): void {
  assert.notEqual(result.status, 0, "The changed input must refuse installation");
  assert.match(result.output, category, result.output);
  assert.doesNotMatch(result.output, /ERR_PNPM_(?:NO_OFFLINE_META|NO_OFFLINE_TARBALL|FETCH_[A-Z_]+)/, "A cache miss is not a policy refusal");
  assert.equal(existsSync(join(context.directory, "verification-witness.json")), false, "Refusal must stop downstream verification");
  assert.equal(existsSync(context.marker), false, "Refusal must not execute the dependency script");
}

/** Requires a usable installation without changing frozen ownership inputs. */
export function assertInstallAccepted(context: InstallFixture, result: { status: number | null; output: string }, expectedCI: CallerCI = { kind: "unset" }): void {
  assert.equal(result.status, 0, result.output);
  assert.match(result.output, /Scope: all 3 workspace projects/, "The actual workspace installer must run");
  assert.doesNotMatch(result.output, purgePrompt, "Automation must not ask for purge confirmation");
  assert.equal(existsSync(context.marker), false, "Reviewed blocked script must not run");
  const witness: unknown = JSON.parse(readFileSync(join(context.directory, "verification-witness.json"), "utf8"));
  assert.deepEqual(witness, expectedCI);
}

/** Resolves the existing pinned Lefthook executable without changing the fixture's module. */
export function readLefthookExecutable(): string {
  const result = runInstallCommand("go", ["tool", "-n", "lefthook"], projectRoot, process.env);
  assert.equal(result.status, 0, result.output);
  const executable = result.stdout.trim();
  assert.notEqual(executable, "");
  assert.equal(existsSync(executable), true, `The pinned Lefthook executable must exist:\n${result.output}`);
  return executable;
}
