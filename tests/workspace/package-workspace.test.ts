import assert from "node:assert/strict";
import { existsSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

import { addReviewedSyntheticPackage, assertInstallRefused, clearInstalledModules, install, installBaseline, members, readPackageManifest, requireJsonRecord, runCommand, syntheticName, withWorkspaceFixture, workspaceRoot } from "#workspace/workspace-fixture";
import { runWorkspaceProbe } from "#workspace/workspace-probe-fixture";

/** Reads a runtime module witness without trusting JSON output. */
function readModuleWitness(output: string): { version: string; module: string } {
  const parsed: unknown = JSON.parse(output);
  requireJsonRecord(parsed);
  const { version, module } = parsed;
  assert.ok(typeof version === "string" && typeof module === "string");
  return { version, module };
}

test("fresh root frozen install gives both members the same usable Zod module", async () => {
  await withWorkspaceFixture(async (context) => {
    await installBaseline(context);
    const modules: { version: string; module: string }[] = [];
    for (const member of members) {
      const result = await runWorkspaceProbe(context, member, { kind: "module" });
      assert.equal(result.status, 0, result.output);
      modules.push(readModuleWitness(result.output));
    }
    assert.equal(modules.length, 2);
    const [first, second] = modules;
    assert.ok(first !== undefined && second !== undefined);
    assert.equal(first.version, "4.6.6");
    assert.deepEqual(second, first, "Both members must resolve the same physical Zod module and version");
  });
});

test("member commands retain their package ownership and shared compiler tools", async () => {
  await withWorkspaceFixture(async (context) => {
    await installBaseline(context);
    for (const member of members) {
      const result = await runWorkspaceProbe(context, member, { kind: "member", name: member.name, directory: join(context.directory, member.directory), scripts: member.scripts });
      assert.equal(result.status, 0, result.output);
      for (const [tool, version] of [["tsc", "7.0.2"], ["oxlint", "1.85.0"]]) {
        assert.ok(tool !== undefined && version !== undefined);
        const toolResult = await runCommand(context, "pnpm", ["--filter", member.name, "exec", tool, "--version"]);
        assert.equal(toolResult.status, 0, toolResult.output);
        assert.match(toolResult.output, new RegExp(`\\b${version.replaceAll(".", "\\.")}\\b`));
      }
    }
  });
});

test("fresh and repeated frozen installs leave the root lock unchanged", async () => {
  await withWorkspaceFixture(async (context) => {
    await installBaseline(context);
    const before = readFileSync(join(workspaceRoot, "pnpm-lock.yaml"));
    assert.deepEqual(readFileSync(join(context.directory, "pnpm-lock.yaml")), before);
    clearInstalledModules(context);
    await installBaseline(context);
    assert.deepEqual(readFileSync(join(context.directory, "pnpm-lock.yaml")), before);
    assert.deepEqual(readFileSync(join(workspaceRoot, "pnpm-lock.yaml")), before);
  });
});

test("reviewed blocked dependency remains usable without running its script", async () => {
  await withWorkspaceFixture(async (context) => {
    await installBaseline(context);
    await addReviewedSyntheticPackage(context);
    const member = members.find(({ name }) => name === "portcullis-load-tests");
    assert.ok(member !== undefined);
    const result = await runWorkspaceProbe(context, member, { kind: "synthetic", packageName: syntheticName });
    assert.equal(result.status, 0, result.output);
    assert.equal(existsSync(context.marker), false);
  });
});

test("fresh frozen install refuses drift in either child manifest", async (scenario) => {
  for (const member of members) {
    await scenario.test(member.name, async () => {
      await withWorkspaceFixture(async (context) => {
        await installBaseline(context);
        const lockBefore = readFileSync(join(context.directory, "pnpm-lock.yaml"));
        const manifestPath = join(context.directory, member.directory, "package.json");
        const manifest = readPackageManifest(manifestPath);
        const dependencies = manifest.dependencies;
        requireJsonRecord(dependencies);
        assert.equal(typeof dependencies.zod, "string");
        dependencies.zod = "4.6.5";
        writeFileSync(manifestPath, JSON.stringify(manifest));
        await assertInstallRefused(context, "ERR_PNPM_OUTDATED_LOCKFILE", lockBefore);
      });
    });
  }
});

test("fresh frozen install refuses shared catalog drift", async () => {
  await withWorkspaceFixture(async (context) => {
    await installBaseline(context);
    const lockBefore = readFileSync(join(context.directory, "pnpm-lock.yaml"));
    const policyPath = join(context.directory, "pnpm-workspace.yaml");
    const policy = readFileSync(policyPath, "utf8");
    const changed = policy.replace(/^([ \t]+zod:[ \t]*)['"]?4\.6\.6['"]?[ \t]*$/m, (_line: string, prefix: string) => `${prefix}4.6.5`);
    assert.notEqual(changed, policy, "The shared default catalog must contain Zod 4.6.6");
    writeFileSync(policyPath, changed);
    const refusal = await assertInstallRefused(context, "ERR_PNPM_LOCKFILE_CONFIG_MISMATCH", lockBefore);
    assert.match(refusal.output, /"catalogs" configuration/);
  });
});

test("fresh frozen install refuses a missing root lock", async () => {
  await withWorkspaceFixture(async (context) => {
    await installBaseline(context);
    rmSync(join(context.directory, "pnpm-lock.yaml"));
    clearInstalledModules(context);
    const result = await install(context);
    assert.notEqual(result.status, 0);
    assert.match(result.output, /\bERR_PNPM_LOCKFILE_CONFIG_MISMATCH\b/);
    assert.match(result.output, /"overrides" configuration/);
    assert.equal(existsSync(join(context.directory, "pnpm-lock.yaml")), false);
    assert.equal(existsSync(context.marker), false);
  });
});

test("fresh frozen install refuses an unreviewed dependency script", async () => {
  await withWorkspaceFixture(async (context) => {
    await installBaseline(context);
    await addReviewedSyntheticPackage(context);
    const lockBefore = readFileSync(join(context.directory, "pnpm-lock.yaml"));
    const policyPath = join(context.directory, "pnpm-workspace.yaml");
    const policy = readFileSync(policyPath, "utf8");
    writeFileSync(policyPath, policy.replace(`  ${syntheticName}: false\n`, ""));
    const refusal = await assertInstallRefused(context, "ERR_PNPM_IGNORED_BUILDS", lockBefore);
    assert.match(refusal.output, new RegExp(`\\b${syntheticName}\\b`));
  });
});
