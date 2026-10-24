import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const workspaceRoot = fileURLToPath(new URL("../..", import.meta.url));
const members = [
  { directory: "web", name: "portcullis-web", scripts: ["build", "typecheck", "lint", "test", "e2e"] },
  { directory: "tests/load", name: "portcullis-load-tests", scripts: ["bundle", "typecheck", "lint", "test"] },
];
const syntheticName = "portcullis-synthetic-build";

/** Run package-manager commands with diagnostics and a bounded lifetime. */
function runCommand(command, arguments_, directory, environment = {}) {
  const result = spawnSync(command, arguments_, {
    cwd: directory,
    encoding: "utf8",
    timeout: 90000,
    env: { ...process.env, CI: "true", ...environment },
  });
  assert.ifError(result.error);
  assert.equal(result.signal, null, `Command was terminated: ${command}`);
  return { status: result.status, output: result.stdout + result.stderr };
}

/** Copy manifests and the root policy without inheriting installed modules. */
function withWorkspaceFixture(scenario) {
  const fixture = mkdtempSync(join(tmpdir(), "portcullis-workspace-"));
  try {
    const sources = ["package.json", "pnpm-workspace.yaml", "pnpm-lock.yaml", ...members.map(({ directory }) => `${directory}/package.json`)];
    for (const source of sources) {
      const origin = join(workspaceRoot, source);
      if (existsSync(origin)) {
        const destination = join(fixture, source);
        mkdirSync(dirname(destination), { recursive: true });
        copyFileSync(origin, destination);
      }
    }
    const store = runCommand("pnpm", ["-C", "web", "store", "path"], workspaceRoot);
    assert.equal(store.status, 0, store.output);
    assert.notEqual(store.output.trim(), "", "pnpm must report its configured store");
    const marker = join(fixture, "unreviewed-script-ran");
    scenario({ fixture, store: store.output.trim(), marker });
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
}

/** Install from the existing store with normal script-policy enforcement. */
function install(context, additionalArguments = ["--frozen-lockfile"]) {
  const result = runCommand("pnpm", ["install", "--offline", `--store-dir=${context.store}`, ...additionalArguments], context.fixture, {
    PORTCULLIS_SCRIPT_MARKER: context.marker,
  });
  assert.doesNotMatch(result.output, /ERR_PNPM_(?:NO_OFFLINE_META|NO_OFFLINE_TARBALL|FETCH_[A-Z_]+)/, `Offline cache failure is not a policy refusal:\n${result.output}`);
  return result;
}

/** Establish that the untouched fixture can install before testing a refusal. */
function installBaseline(context) {
  const result = install(context);
  assert.equal(result.status, 0, `Fresh root frozen install must succeed:\n${result.output}`);
  assert.equal(existsSync(context.marker), false, "No installation script may create a marker");
}

/** Remove every installed module so a second install must enforce fresh policy. */
function clearInstalledModules(context) {
  for (const directory of [context.fixture, ...members.map(({ directory }) => join(context.fixture, directory))]) {
    rmSync(join(directory, "node_modules"), { recursive: true, force: true });
  }
}

/** Refuse the intended configuration category without changing the committed lock. */
function assertInstallRefused(context, category, lockBefore) {
  clearInstalledModules(context);
  const result = install(context);
  assert.notEqual(result.status, 0, "A frozen install must reject the changed input");
  assert.match(result.output, new RegExp(`\\b${category}\\b`), result.output);
  assert.equal(existsSync(context.marker), false, "A refused install must not execute dependency scripts");
  assert.deepEqual(readFileSync(join(context.fixture, "pnpm-lock.yaml")), lockBefore, "Refusal must leave the root lock unchanged");
  return result;
}

/** Add a local package whose usable module needs no installation script. */
function addReviewedSyntheticPackage(context) {
  const packageDirectory = join(context.fixture, "synthetic/package");
  mkdirSync(packageDirectory, { recursive: true });
  writeFileSync(join(packageDirectory, "package.json"), JSON.stringify({
    name: syntheticName,
    version: "1.0.0",
    main: "index.cjs",
    scripts: { postinstall: "node -e \"require('node:fs').writeFileSync(process.env.PORTCULLIS_SCRIPT_MARKER, 'synthetic')\"" },
  }));
  writeFileSync(join(packageDirectory, "index.cjs"), "module.exports = { usable: 'reviewed-without-build' };\n");
  const archive = runCommand("tar", ["-czf", join(context.fixture, "synthetic.tgz"), "-C", join(context.fixture, "synthetic"), "package"], context.fixture);
  assert.equal(archive.status, 0, archive.output);
  const manifestPath = join(context.fixture, "package.json");
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  manifest.dependencies = { ...manifest.dependencies, [syntheticName]: "file:synthetic.tgz" };
  writeFileSync(manifestPath, JSON.stringify(manifest));
  const policyPath = join(context.fixture, "pnpm-workspace.yaml");
  const policy = readFileSync(policyPath, "utf8");
  assert.match(policy, /^allowBuilds:\s*$/m, "The real root policy must declare reviewed build decisions");
  writeFileSync(policyPath, policy.replace(/^allowBuilds:\s*$/m, `allowBuilds:\n  ${syntheticName}: false`));
  const resolution = install(context, ["--lockfile-only", "--no-frozen-lockfile"]);
  assert.equal(resolution.status, 0, resolution.output);
  clearInstalledModules(context);
  installBaseline(context);
}

/** One clean root install must provide one shared Zod module to both members. */
test("fresh root frozen install gives both members the same usable Zod module", () => {
  withWorkspaceFixture((context) => {
    installBaseline(context);
    const modules = members.map(({ directory }) => {
      const result = runCommand(process.execPath, ["--input-type=module", "-e", `
        import assert from 'node:assert/strict';
        import { createRequire } from 'node:module';
        import { realpathSync } from 'node:fs';
        import { z } from 'zod';
        const require = createRequire(import.meta.url);
        assert.deepEqual(z.object({ valid: z.boolean() }).parse({ valid: false }), { valid: false });
        console.log(JSON.stringify({ version: require('zod/package.json').version, module: realpathSync(require.resolve('zod')) }));
      `], join(context.fixture, directory));
      assert.equal(result.status, 0, result.output);
      return JSON.parse(result.output);
    });
    assert.equal(modules[0].version, "4.6.6");
    assert.deepEqual(modules[1], modules[0], "Both members must resolve the same physical Zod module and version");
  });
});

/** Member filters must retain their own command contracts and installed tools. */
test("member commands retain their package ownership and shared compiler tools", () => {
  withWorkspaceFixture((context) => {
    installBaseline(context);
    for (const member of members) {
      const result = runCommand("pnpm", ["--filter", member.name, "exec", process.execPath, "--input-type=module", "-e", `
        import assert from 'node:assert/strict';
        import { readFileSync } from 'node:fs';
        import { resolve } from 'node:path';
        const manifest = JSON.parse(readFileSync('package.json', 'utf8'));
        assert.equal(manifest.name, ${JSON.stringify(member.name)});
        assert.equal(process.cwd(), resolve(${JSON.stringify(join(context.fixture, member.directory))}));
        for (const operation of ${JSON.stringify(member.scripts)}) assert.equal(typeof manifest.scripts[operation], 'string', operation);
      `], context.fixture);
      assert.equal(result.status, 0, result.output);
      for (const [tool, version] of [["tsc", "7.0.2"], ["oxlint", "1.85.0"]]) {
        const toolResult = runCommand("pnpm", ["--filter", member.name, "exec", tool, "--version"], context.fixture);
        assert.equal(toolResult.status, 0, toolResult.output);
        assert.match(toolResult.output, new RegExp(`\\b${version.replaceAll(".", "\\.")}\\b`));
      }
    }
  });
});

/** Repeated clean installations must preserve the root lock byte for byte. */
test("fresh and repeated frozen installs leave the root lock unchanged", () => {
  withWorkspaceFixture((context) => {
    installBaseline(context);
    const before = readFileSync(join(workspaceRoot, "pnpm-lock.yaml"));
    assert.deepEqual(readFileSync(join(context.fixture, "pnpm-lock.yaml")), before);
    clearInstalledModules(context);
    installBaseline(context);
    assert.deepEqual(readFileSync(join(context.fixture, "pnpm-lock.yaml")), before);
    assert.deepEqual(readFileSync(join(workspaceRoot, "pnpm-lock.yaml")), before);
  });
});

/** A reviewed false decision must skip the script while retaining package use. */
test("reviewed blocked dependency remains usable without running its script", () => {
  withWorkspaceFixture((context) => {
    installBaseline(context);
    addReviewedSyntheticPackage(context);
    const result = runCommand(process.execPath, ["-e", `require('node:assert/strict').equal(require('${syntheticName}').usable, 'reviewed-without-build')`], context.fixture);
    assert.equal(result.status, 0, result.output);
    assert.equal(existsSync(context.marker), false);
  });
});

/** Each child manifest must participate in the root frozen-lock contract. */
test("fresh frozen install refuses drift in either child manifest", async (scenario) => {
  for (const member of members) {
    await scenario.test(member.name, () => {
      withWorkspaceFixture((context) => {
        installBaseline(context);
        const lockBefore = readFileSync(join(context.fixture, "pnpm-lock.yaml"));
        const manifestPath = join(context.fixture, member.directory, "package.json");
        const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
        manifest.dependencies.zod = "4.6.5";
        writeFileSync(manifestPath, JSON.stringify(manifest));
        assertInstallRefused(context, "ERR_PNPM_OUTDATED_LOCKFILE", lockBefore);
      });
    });
  }
});

/** A changed catalog must require an explicit root-lock refresh. */
test("fresh frozen install refuses shared catalog drift", () => {
  withWorkspaceFixture((context) => {
    installBaseline(context);
    const lockBefore = readFileSync(join(context.fixture, "pnpm-lock.yaml"));
    const policyPath = join(context.fixture, "pnpm-workspace.yaml");
    const policy = readFileSync(policyPath, "utf8");
    const changed = policy.replace(/^([ \t]+zod:[ \t]*)['"]?4\.6\.6['"]?[ \t]*$/m, (_line, prefix) => `${prefix}4.6.5`);
    assert.notEqual(changed, policy, "The shared default catalog must contain Zod 4.6.6");
    writeFileSync(policyPath, changed);
    const refusal = assertInstallRefused(context, "ERR_PNPM_LOCKFILE_CONFIG_MISMATCH", lockBefore);
    assert.match(refusal.output, /"catalogs" configuration/);
  });
});

/** Frozen mode must refuse a missing root lock instead of reconstructing it. */
test("fresh frozen install refuses a missing root lock", () => {
  withWorkspaceFixture((context) => {
    installBaseline(context);
    rmSync(join(context.fixture, "pnpm-lock.yaml"));
    clearInstalledModules(context);
    const result = install(context);
    assert.notEqual(result.status, 0);
    assert.match(result.output, /\bERR_PNPM_LOCKFILE_CONFIG_MISMATCH\b/);
    assert.match(result.output, /"overrides" configuration/);
    assert.equal(existsSync(join(context.fixture, "pnpm-lock.yaml")), false);
    assert.equal(existsSync(context.marker), false);
  });
});

/** Unknown scripts must fail a fresh frozen install before producing output. */
test("fresh frozen install refuses an unreviewed dependency script", () => {
  withWorkspaceFixture((context) => {
    installBaseline(context);
    addReviewedSyntheticPackage(context);
    const lockBefore = readFileSync(join(context.fixture, "pnpm-lock.yaml"));
    const policyPath = join(context.fixture, "pnpm-workspace.yaml");
    const policy = readFileSync(policyPath, "utf8");
    writeFileSync(policyPath, policy.replace(`  ${syntheticName}: false\n`, ""));
    assertInstallRefused(context, "ERR_PNPM_IGNORED_BUILDS", lockBefore);
  });
});
