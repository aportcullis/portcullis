import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { clearInstalledModules, installBaseline, resolveWorkspaceStore, runCommand, withWorkspaceFixture, workspaceRoot } from "#workspace/workspace-fixture";
import type { VerificationContext } from "#verification/verification-process";

type StoreConfiguration = "absolute" | "relative" | "environment" | "member-cwd";
type StoreFailure = { kind: "none" } | { kind: "failed"; error: unknown };

/** Qualifies actual PNPM configuration only inside one owned workspace and store family. */
async function withStoreConfiguration(configuration: StoreConfiguration, scenario: (context: VerificationContext, root: string, expected: string) => Promise<void>): Promise<void> {
  const ownedBase = mkdtempSync(join(tmpdir(), "portcullis-store-configuration-"));
  const root = join(ownedBase, "workspace");
  const config = join(ownedBase, "config");
  const environment: NodeJS.ProcessEnv = { ...process.env, COREPACK_ENABLE_NETWORK: "0" };
  delete environment.npm_config_store_dir;
  delete environment.NPM_CONFIG_STORE_DIR;
  const context: VerificationContext = { directory: configuration === "member-cwd" ? join(root, "web") : root, environment, processState: { kind: "idle" } };
  let failure: StoreFailure = { kind: "none" };
  try {
    mkdirSync(join(root, "web"), { recursive: true });
    mkdirSync(join(root, "tests/load"), { recursive: true });
    mkdirSync(config);
    for (const name of ["user.npmrc", "global.npmrc"]) writeFileSync(join(config, name), "");
    for (const key of ["NPM_CONFIG_USERCONFIG", "npm_config_userconfig"]) environment[key] = join(config, "user.npmrc");
    for (const key of ["NPM_CONFIG_GLOBALCONFIG", "npm_config_globalconfig"]) environment[key] = join(config, "global.npmrc");
    const rootManifest: unknown = JSON.parse(readFileSync(join(workspaceRoot, "package.json"), "utf8"));
    assert.ok(typeof rootManifest === "object" && rootManifest !== null && "packageManager" in rootManifest && typeof rootManifest.packageManager === "string");
    writeFileSync(join(root, "package.json"), JSON.stringify({ name: "portcullis-workspace", private: true, packageManager: rootManifest.packageManager }));
    writeFileSync(join(root, "web/package.json"), JSON.stringify({ name: "portcullis-web", private: true }));
    writeFileSync(join(root, "tests/load/package.json"), JSON.stringify({ name: "portcullis-load-tests", private: true }));
    writeFileSync(join(root, "pnpm-workspace.yaml"), "packages:\n  - web\n  - tests/load\n");
    const rootStore = join(ownedBase, "root-store");
    writeFileSync(join(root, ".npmrc"), configuration === "relative" ? "store-dir=./relative-root-store\n" : `store-dir=${rootStore}\n`);
    writeFileSync(join(root, "web/.npmrc"), configuration === "relative" ? "" : `store-dir=${join(ownedBase, "web-store")}\n`);
    let expected = configuration === "relative" ? join(root, "relative-root-store", "v10") : join(rootStore, "v10");
    if (configuration === "environment") {
      const store = join(ownedBase, "environment-store");
      environment.npm_config_store_dir = store;
      environment.NPM_CONFIG_STORE_DIR = store;
      expected = join(store, "v10");
    }
    await scenario(context, root, expected);
  } catch (error: unknown) {
    failure = { kind: "failed", error };
  }
  try {
    assert.equal(context.processState.kind, "idle", `Owned commands remain active; store fixture retained at ${ownedBase}`);
    rmSync(ownedBase, { recursive: true, force: true });
    assert.equal(existsSync(ownedBase), false);
  } catch (error: unknown) {
    if (failure.kind === "failed") throw new AggregateError([failure.error, error], "Store configuration and disposal both failed");
    throw error;
  }
  if (failure.kind === "failed") throw failure.error;
}

for (const configuration of ["absolute", "relative", "environment", "member-cwd"]) {
  assert.ok(configuration === "absolute" || configuration === "relative" || configuration === "environment" || configuration === "member-cwd");
  test(`workspace store follows canonical root configuration: ${configuration}`, async () => {
    await withStoreConfiguration(configuration, async (context, root, expected) => {
      assert.equal(await resolveWorkspaceStore(context, root), expected);
    });
  });
}

test("a fresh frozen baseline primes an owned empty store for later offline installs", async () => {
  await withWorkspaceFixture(async (context) => {
    assert.ok(context.store.kind === "resolved", "The owned empty store must be resolved before the baseline");
    const expectedOwnedStore = join(context.directory, "owned-store", "v10");
    mkdirSync(expectedOwnedStore, { recursive: true });
    assert.equal(realpathSync(context.store.path), realpathSync(expectedOwnedStore), "The cold baseline must use only its newly owned store");
    const lockBefore = readFileSync(join(context.directory, "pnpm-lock.yaml"));
    await installBaseline(context);
    assert.deepEqual(readFileSync(join(context.directory, "pnpm-lock.yaml")), lockBefore);
    clearInstalledModules(context);
    await installBaseline(context);
    assert.deepEqual(readFileSync(join(context.directory, "pnpm-lock.yaml")), lockBefore);
    const usable = await runCommand(context, "pnpm", ["--filter", "portcullis-web", "exec", process.execPath, "--input-type=module", "-e", "import assert from 'node:assert/strict'; import { z } from 'zod'; assert.equal(z.boolean().parse(false), false);"]);
    assert.equal(usable.status, 0, usable.output);
    assert.equal(existsSync(context.marker), false);
  }, "owned-empty");
});
