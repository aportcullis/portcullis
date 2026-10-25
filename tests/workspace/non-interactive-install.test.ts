import assert from "node:assert/strict";
import { existsSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

import {
  assertInstallAccepted,
  assertInstallRefused,
  installThroughPipe,
  memberDirectories,
  packageName,
  withInstallFixture,
} from "#workspace/non-interactive-install-fixture";
import { installThroughHookTerminal } from "#workspace/non-interactive-install-terminal";
import { readPackageManifest, requireJsonRecord } from "#workspace/workspace-fixture";

test("fresh piped installation preserves the caller's absent CI and usable members", async () => {
  await withInstallFixture((context) => assertInstallAccepted(context, installThroughPipe(context)));
});

test("warm piped installation preserves an explicit caller CI value", async () => {
  await withInstallFixture((context) => {
    assertInstallAccepted(context, installThroughPipe(context));
    assertInstallAccepted(context, installThroughPipe(context, { CI: "false" }), { kind: "set", value: "false" });
  });
});

test("real hook terminal completes a frozen store change without a confirmation prompt", async () => {
  await withInstallFixture(async (context) => {
    assertInstallAccepted(context, installThroughPipe(context));
    const result = await installThroughHookTerminal(context, { npm_config_store_dir: join(context.directory, "store-b") });
    assertInstallAccepted(context, result);
    assert.match(result.output, /Recreating /, "Changed store metadata must actually recreate modules");
    assert.ok(readFileSync(join(context.directory, "node_modules/.modules.yaml"), "utf8").includes(join(context.directory, "store-b")), "The new owned store must be recorded");
  });
});

test("piped installation completes a frozen virtual-store change without requiring a terminal", async () => {
  await withInstallFixture((context) => {
    assertInstallAccepted(context, installThroughPipe(context));
    const installedPackage = join(context.directory, "web/node_modules", packageName);
    assert.ok(realpathSync(installedPackage).startsWith(join(context.directory, "node_modules/.pnpm/")));
    const result = installThroughPipe(context, { npm_config_virtual_store_dir: join(context.directory, "alternate-virtual-store") });
    assertInstallAccepted(context, result);
    assert.match(result.output, /Recreating /, "Changed virtual-store metadata must actually recreate modules");
    assert.ok(realpathSync(installedPackage).startsWith(join(context.directory, "alternate-virtual-store/")), "The member must resolve its dependency from the changed virtual store");
  });
});

test("the real recipe refuses a member manifest that disagrees with the frozen lock", async () => {
  await withInstallFixture((context) => {
    assertInstallAccepted(context, installThroughPipe(context));
    const manifestPath = join(context.directory, "web/package.json");
    const manifest = readPackageManifest(manifestPath);
    const { dependencies } = manifest;
    requireJsonRecord(dependencies);
    dependencies[packageName] = "file:../changed.tgz";
    writeFileSync(manifestPath, JSON.stringify(manifest));
    assertInstallRefused(context, installThroughPipe(context), /\bERR_PNPM_OUTDATED_LOCKFILE\b/);
  });
});

test("the real recipe refuses root override configuration drift", async () => {
  await withInstallFixture((context) => {
    assertInstallAccepted(context, installThroughPipe(context));
    const policyPath = join(context.directory, "pnpm-workspace.yaml");
    const policy = readFileSync(policyPath, "utf8");
    assert.match(policy, /^  postcss@8: [^\n]+$/m);
    writeFileSync(policyPath, policy.replace(/^  postcss@8: [^\n]+$/m, "  postcss@8: 0.0.0"));
    const result = installThroughPipe(context);
    assertInstallRefused(context, result, /\bERR_PNPM_LOCKFILE_CONFIG_MISMATCH\b/);
    assert.match(result.output, /overrides/);
  });
});

test("the real recipe refuses an absent frozen root lock without recreating it", async () => {
  await withInstallFixture((context) => {
    assertInstallAccepted(context, installThroughPipe(context));
    rmSync(join(context.directory, "pnpm-lock.yaml"));
    const result = installThroughPipe(context);
    assertInstallRefused(context, result, /Headless installation requires a pnpm-lock\.yaml file/);
    assert.equal(existsSync(join(context.directory, "pnpm-lock.yaml")), false);
  });
});

test("the real recipe refuses an unreviewed local postinstall without running it", async () => {
  await withInstallFixture((context) => {
    assertInstallAccepted(context, installThroughPipe(context));
    const policyPath = join(context.directory, "pnpm-workspace.yaml");
    const policy = readFileSync(policyPath, "utf8");
    assert.match(policy, new RegExp(`^  ${packageName}: false$`, "m"));
    writeFileSync(policyPath, policy.replace(`  ${packageName}: false\n`, ""));
    for (const member of [".", ...memberDirectories]) rmSync(join(context.directory, member, "node_modules"), { recursive: true, force: true });
    assertInstallRefused(context, installThroughPipe(context), /\bERR_PNPM_IGNORED_BUILDS\b/);
  });
});
