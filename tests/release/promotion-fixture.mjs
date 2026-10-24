import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import { createServer, request as httpRequest } from "node:http";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execute = promisify(execFile);
export const registryImage = "registry:3.1.2@sha256:01cd312e67026f06f5d89c3e133a4a9adc7cb9eeb9b1194e0b2954dfd770efd2";
export const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const promoter = join(projectRoot, ".github/scripts/promote-release-image.mjs");
const indexType = "application/vnd.oci.image.index.v1+json";
const manifestType = "application/vnd.oci.image.manifest.v1+json";

/** Calculate the identity of exact registry bytes. */
function contentDigest(bytes) {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

/** Run only bounded fixture commands, retaining actual exit diagnostics. */
export async function command(executable, argumentsList, options = {}) {
  return execute(executable, argumentsList, { timeout: 30_000, maxBuffer: 2 ** 20, ...options });
}

/** Require one bounded response from the real registry. */
export async function registryRequest(origin, path, options = {}) {
  const response = await fetch(`${origin}${path}`, { signal: AbortSignal.timeout(10_000), redirect: "error", ...options });
  const bytes = Buffer.from(await response.arrayBuffer());
  assert.ok(bytes.length <= 2 ** 20, "fixture registry response exceeded its byte budget");
  return { status: response.status, headers: response.headers, bytes };
}

/** Start one loopback-only registry with disposable in-container storage. */
export async function startRegistry(suite) {
  const container = `portcullis-promotion-${randomUUID()}`;
  suite.after(async () => removeOwnedContainer(container));
  const reservation = createServer();
  await new Promise((resolveListen, rejectListen) => { reservation.once("error", rejectListen); reservation.listen(0, "127.0.0.1", resolveListen); });
  const address = reservation.address();
  assert.ok(address !== null && typeof address === "object");
  const port = address.port;
  await new Promise((resolveClose, rejectClose) => reservation.close((error) => { if (error) rejectClose(error); else resolveClose(); }));
  await command("docker", ["run", "--detach", "--name", container, "--publish", `127.0.0.1:${port}:${port}`,
    "--read-only", "--tmpfs", "/var/lib/registry", "--env", `REGISTRY_HTTP_ADDR=0.0.0.0:${port}`,
    "--env", "OTEL_TRACES_EXPORTER=none", registryImage]);
  const { stdout } = await command("docker", ["inspect", "--format", '{{json .NetworkSettings.Ports}}', container]);
  const ports = JSON.parse(stdout);
  assert.equal(typeof ports, "object");
  const bindings = ports[`${port}/tcp`];
  assert.ok(Array.isArray(bindings));
  assert.equal(bindings.length, 1);
  const [binding] = bindings;
  assert.equal(binding.HostIp, "127.0.0.1");
  assert.match(binding.HostPort, /^[0-9]+$/);
  const origin = `http://127.0.0.1:${binding.HostPort}`;
  const deadline = Date.now() + 10_000;
  while (true) {
    try {
      const response = await registryRequest(origin, "/v2/");
      if (response.status === 200) break;
    } catch {
      // Startup may not have bound its port yet; the absolute deadline remains fixed.
    }
    assert.ok(Date.now() < deadline, "owned registry did not become ready");
    await new Promise((resolveReady) => setTimeout(resolveReady, 20));
  }
  return { container, origin, repositoryHost: origin.slice("http://".length) };
}

/** Remove the exact owned container and positively qualify absence. */
export async function removeOwnedContainer(container) {
  try {
    await command("docker", ["container", "inspect", container]);
  } catch (error) {
    assert.ok(error instanceof Error && "stderr" in error);
    assert.ok(String(error.stderr).includes(`No such container: ${container}`));
    return;
  }
  await command("docker", ["container", "rm", "--force", container]);
  await assert.rejects(command("docker", ["container", "inspect", container]), (error) => {
    assert.ok(error instanceof Error && "stderr" in error);
    assert.ok(String(error.stderr).includes(`No such container: ${container}`));
    return true;
  });
}

/** Upload a real content-addressed blob through the distribution API. */
export async function uploadBlob(registry, repository, bytes) {
  const digest = contentDigest(bytes);
  const started = await registryRequest(registry.origin, `/v2/${repository}/blobs/uploads/`, { method: "POST" });
  assert.equal(started.status, 202);
  const location = started.headers.get("location");
  assert.equal(typeof location, "string");
  const uploadURL = new URL(location, registry.origin);
  assert.equal(uploadURL.origin, registry.origin);
  uploadURL.searchParams.set("digest", digest);
  const finished = await fetch(uploadURL, { method: "PUT", body: bytes,
    headers: { "Content-Type": "application/octet-stream" }, signal: AbortSignal.timeout(10_000) });
  assert.equal(finished.status, 201);
  await finished.arrayBuffer();
  return { digest, size: bytes.length };
}

/** Upload exact OCI manifest bytes and retain independent hash witnesses. */
export async function uploadManifest(registry, repository, document, mediaType, witnesses) {
  const bytes = Buffer.from(JSON.stringify(document));
  const digest = contentDigest(bytes);
  const response = await registryRequest(registry.origin, `/v2/${repository}/manifests/${digest}`,
    { method: "PUT", body: bytes, headers: { "Content-Type": mediaType } });
  assert.equal(response.status, 201);
  assert.equal(response.headers.get("docker-content-digest"), digest);
  witnesses.push({ digest, bytes, mediaType });
  return { mediaType, digest, size: bytes.length };
}

/** Seed an actual index containing both platform and attestation manifests. */
export async function seedCandidate(registry, repository, label, architectures = ["amd64", "arm64"]) {
  const witnesses = [];
  const manifests = [];
  for (const architecture of architectures) {
    const configuration = await uploadBlob(registry, repository, Buffer.from(JSON.stringify({ architecture, os: "linux",
      config: { User: "65532", Labels: { fixture: label } }, rootfs: { type: "layers", diff_ids: [] } })));
    const platform = await uploadManifest(registry, repository, { schemaVersion: 2, mediaType: manifestType,
      config: { mediaType: "application/vnd.oci.image.config.v1+json", ...configuration }, layers: [] }, manifestType, witnesses);
    manifests.push({ ...platform, platform: { architecture, os: "linux" } });
    const statement = Buffer.from(JSON.stringify({ _type: "https://in-toto.io/Statement/v1", subject: [{ name: label,
      digest: { sha256: platform.digest.slice("sha256:".length) } }], predicateType: "https://slsa.dev/provenance/v1",
    predicate: { fixture: label } }));
    const attestationBlob = await uploadBlob(registry, repository, statement);
    const emptyConfiguration = await uploadBlob(registry, repository, Buffer.from("{}"));
    const attestation = await uploadManifest(registry, repository, { schemaVersion: 2, mediaType: manifestType,
      artifactType: "application/vnd.docker.attestation.manifest.v1+json", subject: platform,
      config: { mediaType: "application/vnd.oci.empty.v1+json", ...emptyConfiguration },
      layers: [{ mediaType: "application/vnd.in-toto+json", ...attestationBlob }] }, manifestType, witnesses);
    manifests.push({ ...attestation, platform: { architecture: "unknown", os: "unknown" }, annotations: {
      "vnd.docker.reference.type": "attestation-manifest", "vnd.docker.reference.digest": platform.digest } });
  }
  const descriptor = await uploadManifest(registry, repository, { schemaVersion: 2, mediaType: indexType, manifests }, indexType, witnesses);
  return { ...descriptor, witnesses };
}

/** Create independent Git evidence without touching the product checkout's refs. */
export async function createSourceFixture(directory, releaseTag) {
  const origin = join(directory, "origin.git");
  const work = join(directory, "work");
  const environment = Object.fromEntries(Object.entries(process.env).filter(([name]) => !name.startsWith("GIT_") && !name.startsWith("REGISTRY_")));
  Object.assign(environment, { GIT_CONFIG_NOSYSTEM: "1", GIT_ATTR_NOSYSTEM: "1", GIT_TERMINAL_PROMPT: "0",
    GIT_CONFIG_GLOBAL: join(directory, "gitconfig"), GIT_AUTHOR_NAME: "Promotion fixture",
    GIT_AUTHOR_EMAIL: "fixture@example.invalid", GIT_COMMITTER_NAME: "Promotion fixture", GIT_COMMITTER_EMAIL: "fixture@example.invalid" });
  await writeFile(environment.GIT_CONFIG_GLOBAL, "");
  const hooks = join(directory, "hooks");
  const templates = join(directory, "templates");
  const attributes = join(directory, "attributes");
  const excludes = join(directory, "excludes");
  await Promise.all([mkdir(hooks), mkdir(templates), writeFile(attributes, ""), writeFile(excludes, "")]);
  environment.GIT_TEMPLATE_DIR = templates;
  for (const [key, value] of [["core.hooksPath", hooks], ["core.attributesFile", attributes],
    ["core.excludesFile", excludes], ["commit.gpgsign", "false"], ["tag.gpgsign", "false"]]) {
    await command("git", ["config", "--file", environment.GIT_CONFIG_GLOBAL, key, value], { env: environment });
  }
  await command("git", ["init", "--quiet", "--bare", "--initial-branch=main", origin], { env: environment });
  await command("git", ["init", "--quiet", "--initial-branch=main", work], { env: environment });
  const git = async (...argumentsList) => command("git", argumentsList, { cwd: work, env: environment });
  await git("commit", "--quiet", "--allow-empty", "-m", "feat: prepare promotion fixture");
  await git("tag", "--annotate", releaseTag, "--message", "Initial fixture annotation");
  await git("branch", "release/v1.2");
  await git("remote", "add", "origin", origin);
  await git("push", "--quiet", "origin", "main", "release/v1.2", `refs/tags/${releaseTag}`);
  const { stdout: sourceSha } = await git("rev-parse", "HEAD");
  const { stdout: tagOid } = await git("rev-parse", `refs/tags/${releaseTag}`);
  return { work, environment, git, sourceSha: sourceSha.trim(), tagOid: tagOid.trim(), releaseTag };
}

/** Execute the real promotion entrypoint and retain its actual process status. */
export async function promote(registry, repository, candidate, source) {
  return (await promotionResult(registry, repository, candidate, source)).status;
}

/** Retain machine output so late refusal cannot look like successful publication. */
export async function promotionResult(registry, repository, candidate, source) {
  try {
    const { stdout, stderr } = await command(process.execPath, [promoter, `${registry.repositoryHost}/${repository}`, candidate.digest,
      source.releaseTag, source.sourceSha, source.tagOid], { cwd: source.work, env: source.environment });
    return { status: 0, stdout, stderr };
  } catch (error) {
    assert.ok(error instanceof Error && "code" in error && typeof error.code === "number" && "stdout" in error && "stderr" in error,
      "promotion failed outside its executable boundary");
    return { status: error.code, stdout: error.stdout, stderr: error.stderr };
  }
}

/** Read a real tag or digest without interpreting unavailable evidence as absence. */
export async function readManifest(registry, repository, reference) {
  const response = await registryRequest(registry.origin, `/v2/${repository}/manifests/${reference}`,
    { headers: { Accept: `${indexType},${manifestType},application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.docker.distribution.manifest.v2+json` } });
  assert.equal(response.status, 200);
  const bytes = response.bytes;
  return { digest: contentDigest(bytes), bytes };
}

/** Positively prove no alias exists with every supported manifest representation admitted. */
export async function requireAbsentAliases(registry, repository, aliases) {
  for (const alias of aliases) {
    const response = await registryRequest(registry.origin, `/v2/${repository}/manifests/${alias}`, { headers: {
      Accept: `${indexType},${manifestType},application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.docker.distribution.manifest.v2+json`,
    } });
    assert.equal(response.status, 404, `forbidden alias ${alias} was published`);
    const document = JSON.parse(response.bytes.toString("utf8"));
    assert.ok(Array.isArray(document.errors) && document.errors.length > 0);
    assert.ok(document.errors.every((error) => error.code === "MANIFEST_UNKNOWN"));
  }
}

/** Prove a digest-only repository has no tag directory or an empty tag census. */
export async function requireNoRegistryTags(registry, repository) {
  const response = await registryRequest(registry.origin, `/v2/${repository}/tags/list`);
  const document = JSON.parse(response.bytes.toString("utf8"));
  if (response.status === 404) {
    assert.ok(Array.isArray(document.errors) && document.errors.length > 0);
    assert.ok(document.errors.every((error) => error.code === "NAME_UNKNOWN"));
  } else {
    assert.equal(response.status, 200);
    assert.ok(document.tags === null || (Array.isArray(document.tags) && document.tags.length === 0));
  }
}

/** Forward real registry operations, failing a read only after every alias was written. */
export async function postcheckFailureProxy(scenario, registry, aliases) {
  let failing = true;
  const published = new Set();
  const server = createServer((incoming, outgoing) => {
    assert.equal(typeof incoming.url, "string");
    const target = new URL(incoming.url, registry.origin);
    const reference = target.pathname.split("/").at(-1);
    if (failing && published.size === aliases.length && incoming.method === "GET" && reference === aliases[1]) {
      outgoing.writeHead(503, { "Content-Type": "application/json" });
      outgoing.end('{"errors":[{"code":"UNAVAILABLE"}]}');
      return;
    }
    const upstream = httpRequest(target, { method: incoming.method, headers: incoming.headers }, (response) => {
      assert.equal(typeof response.statusCode, "number");
      if (incoming.method === "PUT" && response.statusCode === 201 && aliases.includes(reference)) published.add(reference);
      outgoing.writeHead(response.statusCode, response.headers);
      response.pipe(outgoing);
    });
    upstream.on("error", () => { outgoing.writeHead(502); outgoing.end(); });
    incoming.pipe(upstream);
  });
  scenario.after(async () => {
    server.closeAllConnections();
    await new Promise((resolveClose, rejectClose) => server.close((error) => {
      if (error) rejectClose(error);
      else resolveClose();
    }));
  });
  await new Promise((resolveListen, rejectListen) => { server.once("error", rejectListen); server.listen(0, "127.0.0.1", resolveListen); });
  const address = server.address();
  assert.ok(address !== null && typeof address === "object");
  return { repositoryHost: `127.0.0.1:${address.port}`, stopFailing() { failing = false; } };
}
