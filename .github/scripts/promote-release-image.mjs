import { execFile } from "node:child_process";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { createRegistryClient, isRecord, refuseRelease, sha256Pattern } from "./release-registry.mjs";

const execute = promisify(execFile);

/** Execute one fixed release operation without exposing opaque command diagnostics. */
async function runReleaseCommand(executable, argumentsList, diagnostic) {
  try {
    await execute(executable, argumentsList, { timeout: 60_000, maxBuffer: 2 ** 20 });
  } catch {
    refuseRelease(diagnostic);
  }
}

/** Require one supported runnable descriptor per platform while preserving attestations. */
function requireCandidateIndex(document) {
  if (document.schemaVersion !== 2 || document.mediaType !== "application/vnd.oci.image.index.v1+json" ||
    !Array.isArray(document.manifests) || document.manifests.length === 0 || document.manifests.length > 64) {
    refuseRelease("Candidate must be a bounded OCI image index.");
  }
  const platforms = new Set();
  const attestations = [];
  const runnableDigests = new Set();
  for (const descriptor of document.manifests) {
    if (!isRecord(descriptor) || descriptor.mediaType !== "application/vnd.oci.image.manifest.v1+json" ||
      typeof descriptor.digest !== "string" || !sha256Pattern.test(descriptor.digest) ||
      !Number.isSafeInteger(descriptor.size) || descriptor.size < 1 || !isRecord(descriptor.platform)) {
      refuseRelease("Candidate index descriptor is invalid.");
    }
    const { os, architecture } = descriptor.platform;
    if (os === "linux" && (architecture === "amd64" || architecture === "arm64")) {
      if (platforms.has(architecture)) refuseRelease("Candidate platform is ambiguous.");
      platforms.add(architecture);
      runnableDigests.add(descriptor.digest);
    } else if (os === "unknown" && architecture === "unknown" && isRecord(descriptor.annotations) &&
      descriptor.annotations["vnd.docker.reference.type"] === "attestation-manifest" &&
      typeof descriptor.annotations["vnd.docker.reference.digest"] === "string") {
      attestations.push(descriptor.annotations["vnd.docker.reference.digest"]);
    } else {
      refuseRelease("Candidate contains an unqualified platform.");
    }
  }
  if (!platforms.has("amd64") || !platforms.has("arm64") ||
    ![...runnableDigests].every((digest) => attestations.includes(digest)) ||
    !attestations.every((digest) => runnableDigests.has(digest))) {
    refuseRelease("Candidate must retain both supported platforms and their attestations.");
  }
}

/** Promote only a source-fenced verified index, then qualify every written alias. */
async function promoteReleaseImage() {
  const argumentsList = process.argv.slice(2);
  if (argumentsList.length !== 5) refuseRelease("Expected IMAGE_REPOSITORY DIGEST RELEASE_TAG SOURCE_SHA TAG_OID.");
  const [repository, digest, releaseTag, sourceSha, tagOid] = argumentsList;
  const objectIdPattern = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
  if (!sha256Pattern.test(digest) || !objectIdPattern.test(sourceSha) || !objectIdPattern.test(tagOid)) {
    refuseRelease("Candidate digest and admitted Git identities must be complete.");
  }
  await runReleaseCommand("bash", [fileURLToPath(new URL("./validate-release-tag.sh", import.meta.url)), releaseTag],
    "Release tag is invalid.");
  const client = createRegistryClient(repository);
  const candidate = await client.lookup(digest);
  if (candidate.kind !== "found") refuseRelease("Candidate index is unavailable.");
  requireCandidateIndex(candidate.document);
  const fullVersion = releaseTag.slice(1);
  const [major, minor] = fullVersion.split(".");
  const aliases = [fullVersion];
  if (!fullVersion.includes("-")) aliases.push(`${major}.${minor}`);
  aliases.push(`sha-${sourceSha}`);
  const existing = await client.lookup(fullVersion);
  if (existing.kind === "found" && existing.digest !== digest) refuseRelease("Full release version already identifies another digest.");
  const copyArguments = ["buildx", "imagetools", "create"];
  for (const alias of aliases) copyArguments.push("--tag", `${client.repository}:${alias}`);
  copyArguments.push(`${client.repository}@${digest}`);

  await runReleaseCommand("bash", [fileURLToPath(new URL("./verify-release-source.sh", import.meta.url)), releaseTag, sourceSha, tagOid],
    "Admitted release source changed or is unavailable.");
  await runReleaseCommand("docker", copyArguments, "Verified index promotion failed.");
  for (const alias of aliases) {
    const promoted = await client.lookup(alias);
    if (promoted.kind !== "found" || promoted.digest !== digest || !promoted.bytes.equals(candidate.bytes)) {
      refuseRelease("Promoted alias does not preserve the candidate index.");
    }
  }
  process.stdout.write(`${JSON.stringify({ digest, tags: aliases.map((alias) => `${client.repository}:${alias}`) })}\n`);
}

promoteReleaseImage().catch((error) => {
  const diagnostic = error instanceof Error && error.name === "ReleaseImageError" ? error.message : "Release image evidence is invalid.";
  process.stderr.write(`Release image promotion refused: ${diagnostic}\n`);
  process.exitCode = 1;
});
