import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { access, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { ownedBuildxBuilder } from "./buildx-fixture.mjs";

import { command, createSourceFixture, promote, projectRoot, readManifest, registryRequest,
  removeOwnedContainer, requireAbsentAliases, requireNoRegistryTags, startRegistry, uploadBlob, uploadManifest } from "./promotion-fixture.mjs";

/** Remove only the digest references pulled by this invocation. */
async function removePulledReference(reference) {
  try {
    await command("docker", ["image", "inspect", reference]);
  } catch (error) {
    assert.ok(error instanceof Error && "stderr" in error && String(error.stderr).includes(`No such image: ${reference}`));
    return;
  }
  await command("docker", ["image", "rm", "--force", reference]);
  await assert.rejects(command("docker", ["image", "inspect", reference]), (error) => {
    assert.ok(error instanceof Error && "stderr" in error && String(error.stderr).includes(`No such image: ${reference}`));
    return true;
  });
}

/** Change only a real candidate's selected runtime configuration for a refused smoke case. */
async function alteredRuntimeCandidate(registry, repository, originalIndex, architecture, alterConfiguration) {
  const document = JSON.parse(originalIndex.bytes.toString("utf8"));
  assert.ok(Array.isArray(document.manifests));
  const selected = document.manifests.find((descriptor) => descriptor.platform.os === "linux" && descriptor.platform.architecture === architecture);
  assert.ok(selected !== undefined);
  const selectedManifest = JSON.parse((await readManifest(registry, repository, selected.digest)).bytes.toString("utf8"));
  const configurationResponse = await registryRequest(registry.origin, `/v2/${repository}/blobs/${selectedManifest.config.digest}`);
  assert.equal(configurationResponse.status, 200);
  const configuration = JSON.parse(configurationResponse.bytes.toString("utf8"));
  assert.ok(configuration !== null && typeof configuration === "object" && configuration.config !== null && typeof configuration.config === "object");
  alterConfiguration(configuration.config);
  const configurationDescriptor = await uploadBlob(registry, repository, Buffer.from(JSON.stringify(configuration)));
  const manifest = await uploadManifest(registry, repository, { ...selectedManifest,
    config: { ...selectedManifest.config, ...configurationDescriptor } }, selectedManifest.mediaType, []);
  document.manifests = document.manifests.map((descriptor) => descriptor.digest === selected.digest ? { ...descriptor, ...manifest } : descriptor);
  return uploadManifest(registry, repository, document, document.mediaType, []);
}

test("actual product candidate smoke and unchanged-digest promotion", { timeout: 600_000 }, async (suite) => {
  const directory = await mkdtemp(join(tmpdir(), "portcullis-product-candidate-"));
  let builderState = { kind: "not-created" };
  suite.after(async () => {
    if (builderState.kind === "owned") await builderState.builder.close();
    await rm(directory, { recursive: true });
    await assert.rejects(access(directory), (error) => error instanceof Error && "code" in error && error.code === "ENOENT");
  });
  const registry = await startRegistry(suite);
  const builder = ownedBuildxBuilder(directory, registry);
  builderState = { kind: "owned", builder };
  await builder.start(suite.signal);
  const source = await createSourceFixture(directory, "v1.2.3");
  const repository = "actual-portcullis";
  const imageRepository = `${registry.repositoryHost}/${repository}`;
  const metadataPath = join(directory, "metadata.json");
  await command("docker", ["buildx", "build", "--builder", builder.name, "--platform", "linux/amd64,linux/arm64",
    "--build-arg", `BUILD_VERSION=${source.releaseTag}`, "--provenance=mode=max", "--sbom=true",
    "--output", `type=image,name=${imageRepository},push-by-digest=true,name-canonical=true,push=true,oci-mediatypes=true`,
    "--metadata-file", metadataPath, "."], { cwd: projectRoot, env: builder.environment, signal: suite.signal, timeout: 240_000, maxBuffer: 4 * 2 ** 20 });
  const metadata = JSON.parse(await readFile(metadataPath, "utf8"));
  const digest = metadata["containerimage.digest"];
  assert.equal(typeof digest, "string");
  assert.match(digest, /^sha256:[0-9a-f]{64}$/);
  const candidate = { digest };
  const originalIndex = await readManifest(registry, repository, digest);
  assert.equal(originalIndex.digest, digest);
  const { stdout } = await command("docker", ["info", "--format", "{{.Architecture}}"]);
  const daemonArchitecture = stdout.trim();
  assert.ok(["aarch64", "arm64", "x86_64", "amd64"].includes(daemonArchitecture));
  const architecture = daemonArchitecture === "aarch64" || daemonArchitecture === "arm64" ? "arm64" : "amd64";
  const platform = `linux/${architecture}`;
  const qualifiedPlatforms = new Set();
  let indexEvidenceVerified = false;
  const pulledReferences = new Set();
  const emulatedContainers = [`pc-emulated-config-${randomUUID()}`, `pc-emulated-version-${randomUUID()}`];
  suite.after(async () => { for (const reference of pulledReferences) await removePulledReference(reference); });
  suite.after(async () => { for (const container of emulatedContainers) await removeOwnedContainer(container); });
  const smoke = async (selectedCandidate, expectedTag = source.releaseTag) => {
    pulledReferences.add(`${imageRepository}@${selectedCandidate.digest}`);
    return command("bash", [join(projectRoot, ".github/scripts/check-release-candidate.sh"), imageRepository,
      selectedCandidate.digest, platform, expectedTag], { cwd: source.work, env: source.environment, signal: suite.signal, timeout: 90_000 });
  };

  await suite.test("one candidate retains both real platforms and SBOM/provenance manifests", async () => {
    await requireNoRegistryTags(registry, repository);
    await requireAbsentAliases(registry, repository, ["1.2.3", "1.2", "1", "latest", `sha-${source.sourceSha}`]);
    const index = JSON.parse(originalIndex.bytes.toString("utf8"));
    assert.equal(index.mediaType, "application/vnd.oci.image.index.v1+json");
    for (const supportedArchitecture of ["amd64", "arm64"]) {
      const runnable = index.manifests.filter((descriptor) => descriptor.platform.os === "linux" && descriptor.platform.architecture === supportedArchitecture);
      assert.equal(runnable.length, 1);
      const [runnableManifest] = runnable;
      const attestations = index.manifests.filter((descriptor) => descriptor.platform.os === "unknown" &&
        descriptor.annotations["vnd.docker.reference.digest"] === runnableManifest.digest);
      assert.ok(attestations.length > 0);
      const predicateTypes = new Set();
      for (const attestation of attestations) {
        const manifest = JSON.parse((await readManifest(registry, repository, attestation.digest)).bytes.toString("utf8"));
        for (const layer of manifest.layers) {
          const response = await registryRequest(registry.origin, `/v2/${repository}/blobs/${layer.digest}`);
          assert.equal(response.status, 200);
          const statement = JSON.parse(response.bytes.toString("utf8"));
          predicateTypes.add(statement.predicateType);
        }
      }
      assert.ok(predicateTypes.has("https://spdx.dev/Document"));
      assert.ok(predicateTypes.has("https://slsa.dev/provenance/v1") || predicateTypes.has("https://slsa.dev/provenance/v0.2"));
    }
    indexEvidenceVerified = true;
  });
  await suite.test("the exact candidate executes on its actual native daemon", async () => {
    const { stdout: result } = await smoke(candidate);
    assert.ok(result.includes(`${imageRepository}@${digest}`));
    assert.ok(result.includes(platform));
    qualifiedPlatforms.add(platform);
  });
  await suite.test("the exact same candidate also executes the other platform under explicit local emulation", async () => {
    const emulatedPlatform = architecture === "arm64" ? "linux/amd64" : "linux/arm64";
    const reference = `${imageRepository}@${digest}`;
    await command("docker", ["pull", "--platform", emulatedPlatform, reference], { signal: suite.signal, timeout: 90_000 });
    const { stdout: selectedPlatform } = await command("docker", ["image", "inspect", "--platform", emulatedPlatform,
      "--format", "{{.Os}}/{{.Architecture}}", reference]);
    assert.equal(selectedPlatform.trim(), emulatedPlatform);
    const { stdout: runtimeUser } = await command("docker", ["image", "inspect", "--platform", emulatedPlatform,
      "--format", "{{.Config.User}}", reference]);
    assert.ok(["65532", "65532:65532", "nonroot"].includes(runtimeUser.trim()));
    for (const [position, format] of [[0, "invalid"], [1, "json"]]) {
      let refusal;
      try {
        await command("docker", ["run", "--name", emulatedContainers[position], "--rm", "--pull=never", "--platform", emulatedPlatform,
          "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
          "--env", `PORTCULLIS_LOG_FORMAT=${format}`, reference], { signal: suite.signal });
        assert.fail("emulated entrypoint unexpectedly accepted missing configuration");
      } catch (error) {
        assert.ok(error instanceof Error && "code" in error && error.code === 1 && "stderr" in error && typeof error.stderr === "string" &&
          "stdout" in error && typeof error.stdout === "string");
        refusal = error.stdout + error.stderr;
      }
      if (format === "invalid") {
        assert.ok(refusal.includes("config load failed") && refusal.includes("invalid log_format"));
        assert.ok(!refusal.includes('"version":'));
      } else {
        const records = refusal.trim().split("\n").map((line) => JSON.parse(line));
        const [first] = records;
        assert.equal(first.msg, "starting");
        assert.equal(first.level, "INFO");
        assert.equal(first.version, source.releaseTag);
        assert.equal(records.filter((record) => record.msg === "starting").length, 1);
        assert.ok(records.some((record) => record.msg === "master key required"));
        assert.ok(records.slice(1).every((record) => !("version" in record)));
      }
    }
    qualifiedPlatforms.add(emulatedPlatform);
  });
  await suite.test("the same candidate refuses a wrong expected linked version", async () => {
    await assert.rejects(smoke(candidate, "v1.2.4"), (error) => {
      assert.ok(error instanceof Error && "stderr" in error && String(error.stderr).includes("exact linked startup identity"));
      return true;
    });
  });
  await suite.test("a real root-runtime configuration cannot pass", async () => {
    const rootCandidate = await alteredRuntimeCandidate(registry, repository, originalIndex, architecture, (configuration) => { configuration.User = "0"; });
    await assert.rejects(smoke(rootCandidate), (error) => {
      assert.ok(error instanceof Error && "stderr" in error && String(error.stderr).includes("expected non-root user"));
      return true;
    });
  });
  await suite.test("a real failed entrypoint cannot pass", async () => {
    const brokenCandidate = await alteredRuntimeCandidate(registry, repository, originalIndex, architecture, (configuration) => {
      configuration.Entrypoint = ["/not-the-portcullis-entrypoint"];
    });
    await assert.rejects(smoke(brokenCandidate), (error) => {
      assert.ok(error instanceof Error && "stderr" in error && String(error.stderr).includes("invalid-configuration refusal"));
      return true;
    });
  });
  await suite.test("an absent digest cannot pass native execution", async () => {
    await assert.rejects(smoke({ digest: `sha256:${"0".repeat(64)}` }));
  });
  await suite.test("promotion carbon-copies the already-executed candidate", async () => {
    assert.equal(indexEvidenceVerified, true);
    assert.deepEqual([...qualifiedPlatforms].sort(), ["linux/amd64", "linux/arm64"]);
    assert.equal(await promote(registry, repository, candidate, source), 0);
    for (const alias of ["1.2.3", "1.2", `sha-${source.sourceSha}`]) {
      const promoted = await readManifest(registry, repository, alias);
      assert.equal(promoted.digest, digest);
      assert.deepEqual(promoted.bytes, originalIndex.bytes);
    }
  });
  await suite.test("same-digest replay preserves that original candidate", async () => {
    assert.equal(indexEvidenceVerified, true);
    assert.deepEqual([...qualifiedPlatforms].sort(), ["linux/amd64", "linux/arm64"]);
    assert.equal(await promote(registry, repository, candidate, source), 0);
    assert.deepEqual((await readManifest(registry, repository, "1.2.3")).bytes, originalIndex.bytes);
  });
});
