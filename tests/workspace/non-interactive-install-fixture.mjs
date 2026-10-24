import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const projectRoot = fileURLToPath(new URL("../..", import.meta.url));
export const packageName = "portcullis-install-fixture";
export const memberDirectories = ["web", "tests/load"];
const ownershipPaths = ["package.json", "pnpm-workspace.yaml", "pnpm-lock.yaml", ...memberDirectories.map((member) => `${member}/package.json`)];
const purgePrompt = /will be removed and reinstalled from scratch\. Proceed\?/;

/** Extract the actual install recipe without running Make's hook setup. */
function readWorkspaceInstallRecipe() {
  const lines = readFileSync(join(projectRoot, "Makefile"), "utf8").split("\n");
  const targets = lines.flatMap((line, index) => line === "workspace-install:" ? [index] : []);
  assert.equal(targets.length, 1, "Exactly one workspace-install target must exist");
  const recipe = [];
  for (const line of lines.slice(targets[0] + 1)) {
    if (!line.startsWith("\t")) break;
    recipe.push(line.slice(1));
  }
  assert.equal(recipe.length, 1, "The shared installation must have one nonempty recipe");
  assert.notEqual(recipe[0].trim(), "");
  return recipe[0];
}

/** Run a bounded real command and retain its complete diagnostics. */
function runCommand(command, arguments_, directory, environment) {
  const result = spawnSync(command, arguments_, { cwd: directory, env: environment, encoding: "utf8", timeout: 30000 });
  assert.ifError(result.error);
  assert.equal(result.signal, null, `${command} was terminated`);
  return { status: result.status, stdout: result.stdout, output: result.stdout + result.stderr };
}

/** Build a local-only workspace with its own configuration, tarball and stores. */
function createInstallFixture(directory) {
  for (const path of ["web", "tests/load", "synthetic/package", "store-a", "store-b", "config", "tmp", "hooks", "templates"]) {
    mkdirSync(join(directory, path), { recursive: true });
  }
  for (const name of ["npmrc", "gitconfig", "attributes", "excludes"]) writeFileSync(join(directory, "config", name), "");
  const { PATH, COREPACK_HOME } = process.env;
  assert.equal(typeof PATH, "string", "The pinned package manager must be on PATH");
  const environment = {
    PATH,
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
    PORTCULLIS_SCRIPT_MARKER: join(directory, "script-marker"),
    TERM: "dumb",
    NO_COLOR: "1",
  };
  if (typeof COREPACK_HOME === "string") environment.COREPACK_HOME = COREPACK_HOME;
  const { packageManager } = JSON.parse(readFileSync(join(projectRoot, "package.json"), "utf8"));
  assert.equal(typeof packageManager, "string");
  writeFileSync(join(directory, "package.json"), JSON.stringify({ name: "portcullis-workspace", private: true, packageManager }));
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
    main: "index.cjs",
    scripts: { postinstall: "node -e \"require('node:fs').writeFileSync(process.env.PORTCULLIS_SCRIPT_MARKER, 'ran')\"" },
  }));
  writeFileSync(join(directory, "synthetic/package/index.cjs"), "module.exports = { usable: 'local-without-build' };\n");
  const archive = runCommand("tar", ["-czf", "synthetic.tgz", "-C", "synthetic", "package"], directory, environment);
  assert.equal(archive.status, 0, archive.output);
  const resolution = runCommand("pnpm", ["install", "--lockfile-only", "--no-frozen-lockfile"], directory, { ...environment, CI: "true" });
  assert.equal(resolution.status, 0, resolution.output);
  const witness = `import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { writeFileSync } from 'node:fs';
for (const member of ['web', 'tests/load']) {
  const require = createRequire(new URL('./' + member + '/package.json', import.meta.url));
  assert.equal(require('${packageName}').usable, 'local-without-build');
}
const ci = Object.hasOwn(process.env, 'CI') ? { kind: 'set', value: process.env.CI } : { kind: 'unset' };
writeFileSync('verification-witness.json', JSON.stringify(ci));
`;
  writeFileSync(join(directory, "witness.mjs"), witness);
  writeFileSync(join(directory, "Makefile"), `.PHONY: verify workspace-install\nworkspace-install:\n\t${readWorkspaceInstallRecipe()}\nverify: workspace-install\n\t@${quoteShell(process.execPath)} witness.mjs\n`);
  writeFileSync(join(directory, "lefthook.yml"), "pre-push:\n  follow: true\n  commands:\n    verify:\n      run: 'echo $$ > hook-process-group; exec make verify'\n");
  const initialized = runCommand("git", ["-c", `core.hooksPath=${join(directory, "hooks")}`, "-c", `core.attributesFile=${join(directory, "config/attributes")}`, "-c", `core.excludesFile=${join(directory, "config/excludes")}`, "init", "--quiet", `--template=${join(directory, "templates")}`], directory, environment);
  assert.equal(initialized.status, 0, initialized.output);
  const staged = runCommand("git", ["-c", `core.attributesFile=${join(directory, "config/attributes")}`, "add", "Makefile", "lefthook.yml", "witness.mjs", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "web/package.json", "tests/load/package.json", "synthetic.tgz"], directory, environment);
  assert.equal(staged.status, 0, staged.output);
  const committed = runCommand("git", ["-c", `core.hooksPath=${join(directory, "hooks")}`, "-c", "commit.gpgsign=false", "-c", "user.name=Install fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "feat: prepare isolated install fixture"], directory, environment);
  assert.equal(committed.status, 0, committed.output);
  return { directory, environment };
}

/** Quote one literal argument for the shell used by Make. */
function quoteShell(value) {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

/** Remove only this invocation's fixture and verify that cleanup completed. */
export async function withInstallFixture(scenario) {
  const directory = mkdtempSync(join(tmpdir(), "portcullis-noninteractive-install-"));
  try {
    const context = createInstallFixture(directory);
    await scenario(context);
  } finally {
    rmSync(directory, { recursive: true, force: true });
    assert.equal(existsSync(directory), false, "Owned fixture must be removed");
  }
}

/** Execute the actual extracted recipe before the downstream CI witness. */
export function installThroughPipe(context, changes = {}) {
  const inputs = readOwnershipInputs(context);
  rmSync(join(context.directory, "verification-witness.json"), { force: true });
  const result = runCommand("make", ["verify"], context.directory, { ...context.environment, ...changes });
  assert.deepEqual(readOwnershipInputs(context), inputs, "Installation must preserve lockfile, policy and member manifests");
  return result;
}

/** Snapshot present and absent ownership inputs without normalizing their bytes. */
function readOwnershipInputs(context) {
  return ownershipPaths.map((path) => {
    const input = join(context.directory, path);
    return existsSync(input) ? { path, kind: "present", bytes: readFileSync(input) } : { path, kind: "absent" };
  });
}

/** Refuse the intended policy category before downstream verification or scripts. */
export function assertInstallRefused(context, result, category) {
  assert.notEqual(result.status, 0, "The changed input must refuse installation");
  assert.match(result.output, category, result.output);
  assert.doesNotMatch(result.output, /ERR_PNPM_(?:NO_OFFLINE_META|NO_OFFLINE_TARBALL|FETCH_[A-Z_]+)/, "A cache miss is not a policy refusal");
  assert.equal(existsSync(join(context.directory, "verification-witness.json")), false, "Refusal must stop downstream verification");
  assert.equal(existsSync(context.environment.PORTCULLIS_SCRIPT_MARKER), false, "Refusal must not execute the dependency script");
}

/** Require a usable installation without changing frozen ownership inputs. */
export function assertInstallAccepted(context, result, expectedCI = { kind: "unset" }) {
  assert.equal(result.status, 0, result.output);
  assert.match(result.output, /Scope: all 3 workspace projects/, "The actual workspace installer must run");
  assert.doesNotMatch(result.output, purgePrompt, "Automation must not ask for purge confirmation");
  assert.equal(existsSync(context.environment.PORTCULLIS_SCRIPT_MARKER), false, "Reviewed blocked script must not run");
  assert.deepEqual(JSON.parse(readFileSync(join(context.directory, "verification-witness.json"), "utf8")), expectedCI);
}

/** Resolve the existing pinned Lefthook executable without changing the fixture's module. */
function readLefthookExecutable() {
  const result = runCommand("go", ["tool", "-n", "lefthook"], projectRoot, process.env);
  assert.equal(result.status, 0, result.output);
  const executable = result.stdout.trim();
  assert.notEqual(executable, "");
  assert.equal(existsSync(executable), true, `The pinned Lefthook executable must exist:\n${result.output}`);
  return executable;
}

/** Signal only a process group owned and recorded by this fixture. */
function signalOwnedProcessGroup(processGroup, signal) {
  assert.ok(Number.isSafeInteger(processGroup) && processGroup > 1 && processGroup !== process.pid);
  try {
    process.kill(-processGroup, signal);
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
  }
}

/** Report whether an exact owned process group still exists. */
function ownedProcessGroupExists(processGroup) {
  try {
    process.kill(-processGroup, 0);
    return true;
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
    return false;
  }
}

/** Confirm only recorded groups have stopped before removing their fixture. */
async function requireOwnedProcessGroupsStopped(processGroups) {
  const deadline = Date.now() + 3000;
  while (true) {
    const remaining = [...processGroups].filter(ownedProcessGroupExists);
    if (remaining.length === 0) return;
    for (const processGroup of remaining) signalOwnedProcessGroup(processGroup, "SIGKILL");
    assert.ok(Date.now() < deadline, `Owned hook process groups did not stop: ${remaining.join(", ")}`);
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
}

/** Run real Lefthook with a terminal while keeping hook reference input on a pipe. */
export async function installThroughHookTerminal(context, changes = {}) {
  assert.ok(process.platform === "darwin" || process.platform === "linux", "The hook fixture requires a Unix pseudo-terminal");
  const python = runCommand("python3", ["-I", "-S", "-c", "import pty"], context.directory, context.environment);
  assert.equal(python.status, 0, `Python3 with its standard-library PTY module is required:\n${python.output}`);
  const inputs = readOwnershipInputs(context);
  rmSync(join(context.directory, "verification-witness.json"), { force: true });
  const wrapper = join(context.directory, "hook-wrapper.mjs");
  writeFileSync(wrapper, `import { spawnSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';
writeFileSync('wrapper-process-group', String(process.pid));
const result = spawnSync(${JSON.stringify(readLefthookExecutable())}, ['run', 'pre-push'], {
  stdio: ['pipe', 'inherit', 'inherit'],
  input: ${JSON.stringify(`refs/heads/main ${"a".repeat(40)} refs/heads/main ${"b".repeat(40)}\n`)},
});
if (result.error) throw result.error;
writeFileSync('hook-status.json', JSON.stringify({ status: result.status, signal: result.signal }));
process.exit(typeof result.status === 'number' ? result.status : 1);
`);
  const terminalProgram = "import pty, sys\npty.spawn(sys.argv[1:])\n";
  const child = spawn("python3", ["-I", "-S", "-c", terminalProgram, process.execPath, wrapper], { cwd: context.directory, env: { ...context.environment, ...changes }, detached: true, stdio: "pipe" });
  assert.equal(typeof child.pid, "number");
  const outerProcessGroup = child.pid;
  let output = "";
  let outcome = "completed";
  let stopping = false;
  let forceStop = { kind: "inactive" };
  let callbackFailure = { kind: "none" };
  const processGroups = new Set([outerProcessGroup]);
  const recordOwnedProcessGroups = () => {
    let innermostProcessGroup = outerProcessGroup;
    for (const record of ["wrapper-process-group", "hook-process-group"]) {
      const processGroupPath = join(context.directory, record);
      if (existsSync(processGroupPath)) {
        const processGroup = Number(readFileSync(processGroupPath, "utf8").trim());
        assert.ok(Number.isSafeInteger(processGroup) && processGroup > 1 && processGroup !== process.pid);
        processGroups.add(processGroup);
        innermostProcessGroup = processGroup;
      }
    }
    return innermostProcessGroup;
  };
  const captureCallbackFailure = (error) => {
    callbackFailure = { kind: "failed", error };
    child.kill("SIGKILL");
  };
  const stop = () => {
    if (stopping) return;
    stopping = true;
    forceStop = { kind: "active", timer: setTimeout(() => {
      try {
        recordOwnedProcessGroups();
        for (const processGroup of processGroups) {
          if (ownedProcessGroupExists(processGroup)) signalOwnedProcessGroup(processGroup, "SIGKILL");
        }
      } catch (error) {
        captureCallbackFailure(error);
      }
    }, 2000) };
    const innermostProcessGroup = recordOwnedProcessGroups();
    if (ownedProcessGroupExists(innermostProcessGroup)) signalOwnedProcessGroup(innermostProcessGroup, "SIGTERM");
  };
  const timeout = setTimeout(() => {
    outcome = "timeout";
    try { stop(); } catch (error) { captureCallbackFailure(error); }
  }, 30000);
  const receive = (chunk) => {
    try {
      output += chunk.toString("utf8");
      if (output.length > 2 * 1024 * 1024) { outcome = "excess-output"; stop(); }
      if (outcome === "completed" && purgePrompt.test(output)) {
        outcome = child.exitCode === null && !existsSync(join(context.directory, "hook-status.json")) ? "prompt" : "prompt-after-completion";
        stop();
      }
    } catch (error) {
      captureCallbackFailure(error);
    }
  };
  child.stdout.on("data", receive);
  child.stderr.on("data", receive);
  child.stdin.on("error", (error) => { if (error.code !== "EPIPE") captureCallbackFailure(error); });
  try {
    await new Promise((resolve, reject) => { child.once("error", reject); child.once("close", resolve); });
  } finally {
    clearTimeout(timeout);
    child.stdin.destroy();
    if (forceStop.kind === "active") clearTimeout(forceStop.timer);
    recordOwnedProcessGroups();
    await requireOwnedProcessGroupsStopped(processGroups);
  }
  if (callbackFailure.kind === "failed") throw callbackFailure.error;
  assert.deepEqual(readOwnershipInputs(context), inputs, "The real hook must preserve frozen ownership inputs");
  if (outcome !== "completed") return { status: 1, output: `${outcome}: ${output}` };
  assert.equal(existsSync(join(context.directory, "hook-status.json")), true, `The terminal wrapper must report the actual hook status:\n${output}`);
  const { status, signal } = JSON.parse(readFileSync(join(context.directory, "hook-status.json"), "utf8"));
  assert.equal(signal, null, output);
  return { status, output };
}
