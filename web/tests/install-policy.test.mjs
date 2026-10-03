import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, rmSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { resolve, join } from "node:path";
import test from "node:test";

/** An unknown dependency must never execute its installation script. */
test("fresh install refuses an unreviewed build script", () => {
  const root = resolve("..");
  const fixture = mkdtempSync(join(root, ".test-docker/install-policy-"));
  try {
    const source = join(fixture, "source");
    mkdirSync(join(source, "package"), { recursive: true });
    writeFileSync(join(source, "package/package.json"), JSON.stringify({
      name: "portcullis-synthetic-build", version: "1.0.0",
      scripts: { postinstall: "node -e \"require('node:fs').writeFileSync('script-ran', 'synthetic')\"" },
    }));
    const archive = spawnSync("tar", ["-czf", join(fixture, "dependency.tgz"), "-C", source, "package"], { encoding: "utf8" });
    assert.equal(archive.status, 0, archive.stderr);
    writeFileSync(join(fixture, "package.json"), JSON.stringify({
      private: true, packageManager: "pnpm@10.34.6",
      dependencies: { "portcullis-synthetic-build": "file:dependency.tgz" },
    }));
    const policy = readFileSync("pnpm-workspace.yaml", "utf8");
    writeFileSync(join(fixture, "pnpm-workspace.yaml"), policy);
    writeFileSync(join(fixture, ".npmrc"), `store-dir=${join(root, ".test-docker/pnpm-store")}\n`);
    const result = spawnSync("pnpm", ["install", "--offline"], { cwd: fixture, encoding: "utf8", timeout: 30000 });
    assert.ifError(result.error);
    const output = result.stdout + result.stderr;
    assert.notEqual(result.status, 0, "Unreviewed script installation must fail");
    assert.match(output, /ERR_PNPM_IGNORED_BUILDS/);
    assert.match(output, /portcullis-synthetic-build/);
    assert.equal(existsSync(join(fixture, "node_modules/portcullis-synthetic-build/script-ran")), false);
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});
