import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { delimiter, dirname, join } from "node:path";
import test from "node:test";

import { clearInstalledModules, installBaseline, members, resolveWorkspaceStore, runCommand, withWorkspaceFixture, workspaceRoot } from "#workspace/workspace-fixture";
import { runWorkspaceProbe } from "#workspace/workspace-probe-fixture";
import type { VerificationContext } from "#verification/verification-process";

type StoreConfiguration = "absolute" | "relative" | "environment" | "member-cwd";
type StoreFailure = { kind: "none" } | { kind: "failed"; error: unknown };

/** Qualifies actual PNPM configuration only inside one owned workspace and store family. */
async function withStoreConfiguration(configuration: StoreConfiguration, scenario: (context: VerificationContext, root: string, expected: string) => Promise<void>): Promise<void> {
  const ownedBase = realpathSync(mkdtempSync(join(tmpdir(), "portcullis-store-configuration-")));
  const root = join(ownedBase, "workspace");
  const config = join(ownedBase, "config");
  const pathProbe = join(ownedBase, "path-pnpm-ran");
  const environment: NodeJS.ProcessEnv = { ...process.env, COREPACK_ENABLE_NETWORK: "0" };
  delete environment.npm_config_store_dir;
  delete environment.NPM_CONFIG_STORE_DIR;
  const context: VerificationContext = { directory: configuration === "member-cwd" ? join(root, "web") : root, environment, processState: { kind: "idle" } };
  let failure: StoreFailure = { kind: "none" };
  try {
    mkdirSync(join(root, "web"), { recursive: true });
    mkdirSync(join(root, "tests/load"), { recursive: true });
    mkdirSync(config);
    environment.COREPACK_ROOT = config;
    const shadowDirectory = join(ownedBase, "path-shadow");
    mkdirSync(shadowDirectory);
    writeFileSync(join(shadowDirectory, "pnpm"), '#!/bin/sh\nprintf "unexpected PATH pnpm\\n" > "$PORTCULLIS_PATH_PROBE"\nexit 97\n', { mode: 0o755 });
    const { PATH } = environment;
    assert.ok(typeof PATH === "string");
    environment.PATH = `${shadowDirectory}${delimiter}${PATH}`;
    environment.PORTCULLIS_PATH_PROBE = pathProbe;
    for (const name of ["user.npmrc", "global.npmrc"]) writeFileSync(join(config, name), "");
    for (const key of ["NPM_CONFIG_USERCONFIG", "npm_config_userconfig"]) environment[key] = join(config, "user.npmrc");
    for (const key of ["NPM_CONFIG_GLOBALCONFIG", "npm_config_globalconfig"]) environment[key] = join(config, "global.npmrc");
    const rootManifest: unknown = JSON.parse(readFileSync(join(workspaceRoot, "package.json"), "utf8"));
    assert.ok(typeof rootManifest === "object" && rootManifest !== null && "packageManager" in rootManifest && typeof rootManifest.packageManager === "string");
    writeFileSync(join(root, "package.json"), JSON.stringify({ name: "portcullis-workspace", private: true, packageManager: rootManifest.packageManager, scripts: { "runtime-version": "node --version" } }));
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
    assert.equal(existsSync(pathProbe), false, "Package commands must retain the admitted package manager instead of searching PATH again");
  } catch (error: unknown) {
    failure = { kind: "failed", error };
  }
  try {
    assert.equal(context.processState.kind, "idle", `Owned commands remain active; store fixture retained at ${ownedBase}`);
    await rm(ownedBase, { recursive: true, force: true });
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

for (const operation of ["install", "exec", "config", "run"]) {
  test(`package command retains the admitted launcher with Corepack metadata: ${operation}`, async () => {
    await withStoreConfiguration("absolute", async (context, root, expected) => {
      let arguments_: readonly string[];
      switch (operation) {
        case "install": arguments_ = ["install", "--lockfile-only", "--no-frozen-lockfile", "--offline"]; break;
        case "exec": arguments_ = ["exec", process.execPath, "--version"]; break;
        case "config": arguments_ = ["config", "get", "store-dir"]; break;
        case "run": arguments_ = ["run", "runtime-version"]; break;
        default: assert.fail(`Unsupported package command: ${operation}`);
      }
      const result = await runCommand(context, "pnpm", ["-C", root, ...arguments_]);
      assert.equal(result.status, 0, result.output);
      if (operation === "install") assert.equal(existsSync(join(root, "pnpm-lock.yaml")), true);
      else if (operation === "config") assert.equal(result.output.trim(), dirname(expected));
      else assert.ok(result.output.includes(process.version), result.output);
    });
  });
}

type RefusedLauncher = { name: string; change: (environment: NodeJS.ProcessEnv) => void; category: RegExp };
const refusedLaunchers: readonly RefusedLauncher[] = [
  { name: "missing script launcher", change: (environment) => { delete environment.npm_execpath; }, category: /active pnpm script launcher must be an absolute path/ },
  { name: "relative script launcher", change: (environment) => { environment.npm_execpath = "pnpm"; }, category: /active pnpm script launcher must be an absolute path/ },
  { name: "different package manager", change: (environment) => { environment.npm_config_user_agent = "npm/0.0.0"; }, category: /active package manager must match the declared pnpm version/ },
  { name: "different pnpm version", change: (environment) => { environment.npm_config_user_agent = "pnpm/0.0.0"; }, category: /active package manager must match the declared pnpm version/ },
];

for (const { name, change, category } of refusedLaunchers) {
  test(`store discovery refuses a ${name} before searching PATH`, async () => {
    await withStoreConfiguration("absolute", async (context, root) => {
      change(context.environment);
      await assert.rejects(resolveWorkspaceStore(context, root), category);
      await assert.rejects(runCommand(context, "pnpm", ["-C", root, "--version"]), category);
      assert.equal(context.processState.kind, "idle");
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
    const member = members.find(({ name }) => name === "portcullis-web");
    assert.ok(member !== undefined, "The frontend must remain a declared workspace member");
    const usable = await runWorkspaceProbe(context, member, { kind: "module" });
    assert.equal(usable.status, 0, usable.output);
    assert.equal(existsSync(context.marker), false);
  }, "owned-empty");
});
