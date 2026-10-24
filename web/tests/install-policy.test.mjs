import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, rmSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

/** The actual root policy must reject unknown scripts during frozen installs. */
test("fresh frozen install applies the root build-script policy", () => {
  const root = fileURLToPath(new URL("../..", import.meta.url));
  const fixture = mkdtempSync(join(tmpdir(), "portcullis-install-policy-"));
  try {
    const marker = join(fixture, "unreviewed-script-ran");
    const source = join(fixture, "source");
    mkdirSync(join(source, "package"), { recursive: true });
    writeFileSync(join(source, "package/package.json"), JSON.stringify({
      name: "portcullis-synthetic-build", version: "1.0.0",
      main: "index.cjs",
      scripts: { postinstall: "node -e \"require('node:fs').writeFileSync(process.env.PORTCULLIS_SCRIPT_MARKER, 'synthetic')\"" },
    }));
    writeFileSync(join(source, "package/index.cjs"), "module.exports = 'usable-without-build';\n");
    const archive = spawnSync("tar", ["-czf", join(fixture, "dependency.tgz"), "-C", source, "package"], { encoding: "utf8" });
    assert.ifError(archive.error);
    assert.equal(archive.status, 0, archive.stderr);
    const rootManifest = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
    writeFileSync(join(fixture, "package.json"), JSON.stringify({
      private: true, packageManager: rootManifest.packageManager,
      dependencies: { "portcullis-synthetic-build": "file:dependency.tgz" },
    }));
    const policy = readFileSync(join(root, "pnpm-workspace.yaml"), "utf8");
    assert.match(policy, /^allowBuilds:\s*$/m);
    writeFileSync(join(fixture, "pnpm-workspace.yaml"), policy.replace(/^allowBuilds:\s*$/m, "allowBuilds:\n  portcullis-synthetic-build: false"));
    const store = spawnSync("pnpm", ["-C", "web", "store", "path"], { cwd: root, encoding: "utf8", timeout: 30000 });
    assert.ifError(store.error);
    assert.equal(store.status, 0, store.stderr);
    assert.notEqual(store.stdout.trim(), "");
    const install = (arguments_) => {
      const result = spawnSync("pnpm", ["install", "--offline", `--store-dir=${store.stdout.trim()}`, ...arguments_], {
        cwd: fixture,
        encoding: "utf8",
        timeout: 30000,
        env: { ...process.env, CI: "true", PORTCULLIS_SCRIPT_MARKER: marker },
      });
      assert.ifError(result.error);
      const output = result.stdout + result.stderr;
      assert.doesNotMatch(output, /ERR_PNPM_(?:NO_OFFLINE_META|NO_OFFLINE_TARBALL|FETCH_[A-Z_]+)/);
      return { status: result.status, output };
    };
    const resolution = install(["--lockfile-only", "--no-frozen-lockfile"]);
    assert.equal(resolution.status, 0, resolution.output);
    const baseline = install(["--frozen-lockfile"]);
    assert.equal(baseline.status, 0, baseline.output);
    const usable = spawnSync(process.execPath, ["-e", "require('node:assert/strict').equal(require('portcullis-synthetic-build'), 'usable-without-build')"], { cwd: fixture, encoding: "utf8" });
    assert.ifError(usable.error);
    assert.equal(usable.status, 0, usable.stderr);
    assert.equal(existsSync(marker), false);
    const lockBefore = readFileSync(join(fixture, "pnpm-lock.yaml"));
    rmSync(join(fixture, "node_modules"), { recursive: true, force: true });
    writeFileSync(join(fixture, "pnpm-workspace.yaml"), policy);
    const refusal = install(["--frozen-lockfile"]);
    assert.notEqual(refusal.status, 0, "Unreviewed script installation must fail");
    assert.match(refusal.output, /\bERR_PNPM_IGNORED_BUILDS\b/);
    assert.match(refusal.output, /portcullis-synthetic-build/);
    assert.equal(existsSync(marker), false);
    assert.deepEqual(readFileSync(join(fixture, "pnpm-lock.yaml")), lockBefore);
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});
