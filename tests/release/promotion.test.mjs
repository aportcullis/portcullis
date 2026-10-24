import assert from "node:assert/strict";
import { access, mkdir, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { command, createSourceFixture, postcheckFailureProxy, promote, promotionResult, projectRoot,
  readManifest, requireAbsentAliases, requireNoRegistryTags, seedCandidate, startRegistry } from "./promotion-fixture.mjs";

test("real registry promotion", { timeout: 120_000 }, async (suite) => {
  const directory = await mkdtemp(join(tmpdir(), "portcullis-promotion-"));
  const registry = await startRegistry(suite);
  suite.after(async () => {
    await rm(directory, { recursive: true });
    await assert.rejects(access(directory), (error) => error instanceof Error && "code" in error && error.code === "ENOENT");
  });
  let sequence = 0;
  const fixture = async (releaseTag = "v1.2.3") => {
    sequence++;
    const caseDirectory = join(directory, `case-${sequence}`);
    await mkdir(caseDirectory);
    const source = await createSourceFixture(caseDirectory, releaseTag);
    const repository = `portcullis-${sequence}`;
    const candidate = await seedCandidate(registry, repository, `candidate-${sequence}`);
    return { source, repository, candidate };
  };

  await suite.test("stable aliases preserve the exact index and attestation manifests", async () => {
    const { source, repository, candidate } = await fixture();
    await requireNoRegistryTags(registry, repository);
    assert.equal(await promote(registry, repository, candidate, source), 0);
    await assert.rejects(requireNoRegistryTags(registry, repository));
    for (const alias of ["1.2.3", "1.2", `sha-${source.sourceSha}`]) {
      const promoted = await readManifest(registry, repository, alias);
      assert.equal(promoted.digest, candidate.digest);
      const [indexWitness] = candidate.witnesses.filter(({ digest }) => digest === candidate.digest);
      assert.deepEqual(promoted.bytes, indexWitness.bytes);
    }
    for (const witness of candidate.witnesses) {
      const actual = await readManifest(registry, repository, witness.digest);
      assert.deepEqual(actual.bytes, witness.bytes);
    }
    await requireAbsentAliases(registry, repository, ["1", "latest"]);
  });
  await suite.test("prerelease publishes full and commit references without a minor alias", async () => {
    const { source, repository, candidate } = await fixture("v1.2.4-rc.1");
    assert.equal(await promote(registry, repository, candidate, source), 0);
    assert.equal((await readManifest(registry, repository, "1.2.4-rc.1")).digest, candidate.digest);
    assert.equal((await readManifest(registry, repository, `sha-${source.sourceSha}`)).digest, candidate.digest);
    await requireAbsentAliases(registry, repository, ["1.2", "1", "latest"]);
  });
  await suite.test("same digest replay repairs a mutable alias", async () => {
    const { source, repository, candidate } = await fixture();
    assert.equal(await promote(registry, repository, candidate, source), 0);
    const replacement = await seedCandidate(registry, repository, "replacement");
    await command("docker", ["buildx", "imagetools", "create", "--tag", `${registry.repositoryHost}/${repository}:1.2`,
      `${registry.repositoryHost}/${repository}@${replacement.digest}`]);
    assert.equal(await promote(registry, repository, candidate, source), 0);
    assert.equal((await readManifest(registry, repository, "1.2")).digest, candidate.digest);
    assert.equal((await readManifest(registry, repository, "1.2.3")).digest, candidate.digest);
    assert.equal((await readManifest(registry, repository, `sha-${source.sourceSha}`)).digest, candidate.digest);
  });
  await suite.test("a distinct full version may advance convenience aliases", async () => {
    const { source, repository, candidate } = await fixture();
    assert.equal(await promote(registry, repository, candidate, source), 0);
    await source.git("tag", "--annotate", "v1.2.4", "--message", "Next fixture version");
    await source.git("push", "--quiet", "origin", "refs/tags/v1.2.4");
    const { stdout: tagOid } = await source.git("rev-parse", "refs/tags/v1.2.4");
    const next = await seedCandidate(registry, repository, "next-release");
    assert.equal(await promote(registry, repository, next, { ...source, releaseTag: "v1.2.4", tagOid: tagOid.trim() }), 0);
    assert.equal((await readManifest(registry, repository, "1.2.3")).digest, candidate.digest);
    assert.equal((await readManifest(registry, repository, "1.2")).digest, next.digest);
  });
  await suite.test("an existing full version cannot change digest", async () => {
    const { source, repository, candidate } = await fixture();
    assert.equal(await promote(registry, repository, candidate, source), 0);
    const replacement = await seedCandidate(registry, repository, "conflicting-release");
    assert.notEqual(await promote(registry, repository, replacement, source), 0);
    for (const alias of ["1.2.3", "1.2", `sha-${source.sourceSha}`]) {
      assert.equal((await readManifest(registry, repository, alias)).digest, candidate.digest);
    }
  });
  await suite.test("an index missing ARM64 cannot publish", async () => {
    const { source, repository } = await fixture();
    const incomplete = await seedCandidate(registry, repository, "missing-platform", ["amd64"]);
    assert.notEqual(await promote(registry, repository, incomplete, source), 0);
    await requireAbsentAliases(registry, repository, ["1.2.3", "1.2", `sha-${source.sourceSha}`]);
  });
  await suite.test("a deleted source tag cannot publish", async () => {
    const { source, repository, candidate } = await fixture();
    await source.git("push", "--quiet", "origin", `:refs/tags/${source.releaseTag}`);
    assert.notEqual(await promote(registry, repository, candidate, source), 0);
    await requireAbsentAliases(registry, repository, ["1.2.3", "1.2", `sha-${source.sourceSha}`]);
  });
  await suite.test("same-commit source reannotation cannot publish", async () => {
    const { source, repository, candidate } = await fixture();
    await source.git("tag", "--force", "--annotate", source.releaseTag, "--message", "Replacement annotation");
    await source.git("push", "--quiet", "--force", "origin", `refs/tags/${source.releaseTag}`);
    assert.notEqual(await promote(registry, repository, candidate, source), 0);
    await requireAbsentAliases(registry, repository, ["1.2.3", "1.2", `sha-${source.sourceSha}`]);
  });
  await suite.test("candidate smoke refuses the other native architecture before pulling", async () => {
    const { source, repository, candidate } = await fixture();
    const { stdout } = await command("docker", ["info", "--format", "{{.Architecture}}"]);
    const architecture = stdout.trim();
    assert.ok(["aarch64", "arm64", "x86_64", "amd64"].includes(architecture));
    const otherPlatform = architecture === "aarch64" || architecture === "arm64" ? "linux/amd64" : "linux/arm64";
    await assert.rejects(command("bash", [join(projectRoot, ".github/scripts/check-release-candidate.sh"),
      `${registry.repositoryHost}/${repository}`, candidate.digest, otherPlatform, source.releaseTag],
    { cwd: source.work, env: source.environment }), (error) => {
      assert.ok(error instanceof Error && "stderr" in error && typeof error.stderr === "string" && "stdout" in error);
      assert.ok(error.stderr.split("\n").includes("Release candidate refused: candidate must execute on its native Docker daemon platform"));
      assert.equal(error.stdout, "");
      return true;
    });
  });
  await suite.test("a missing candidate digest cannot publish any alias", async () => {
    const { source, repository } = await fixture();
    assert.notEqual(await promote(registry, repository, { digest: `sha256:${"0".repeat(64)}` }, source), 0);
    await requireAbsentAliases(registry, repository, ["1.2.3", "1.2", `sha-${source.sourceSha}`]);
  });
  await suite.test("an existing full-version single-platform manifest cannot be overwritten", async () => {
    const { source, repository, candidate } = await fixture();
    const [platformManifest] = candidate.witnesses;
    await command("docker", ["buildx", "imagetools", "create", "--prefer-index=false", "--tag",
      `${registry.repositoryHost}/${repository}:1.2.3`, `${registry.repositoryHost}/${repository}@${platformManifest.digest}`]);
    assert.notEqual(await promote(registry, repository, candidate, source), 0);
    assert.equal((await readManifest(registry, repository, "1.2.3")).digest, platformManifest.digest);
    await requireAbsentAliases(registry, repository, ["1.2", `sha-${source.sourceSha}`]);
  });
  await suite.test("a failed alias postcheck emits no success and permits same-digest recovery", async (scenario) => {
    const { source, repository, candidate } = await fixture();
    const aliases = ["1.2.3", "1.2", `sha-${source.sourceSha}`];
    const proxy = await postcheckFailureProxy(scenario, registry, aliases);
    const refused = await promotionResult(proxy, repository, candidate, source);
    assert.notEqual(refused.status, 0);
    assert.equal(refused.stdout, "");
    for (const alias of aliases) assert.equal((await readManifest(registry, repository, alias)).digest, candidate.digest);
    proxy.stopFailing();
    assert.equal(await promote(proxy, repository, candidate, source), 0);
    for (const alias of aliases) assert.equal((await readManifest(registry, repository, alias)).digest, candidate.digest);
  });
});
