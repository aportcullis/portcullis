import { createHash } from "node:crypto";

export const manifestTypes = [
  "application/vnd.oci.image.index.v1+json",
  "application/vnd.oci.image.manifest.v1+json",
  "application/vnd.docker.distribution.manifest.list.v2+json",
  "application/vnd.docker.distribution.manifest.v2+json",
];
export const sha256Pattern = /^sha256:[0-9a-f]{64}$/;

/** Recognize a JSON object before inspecting registry-controlled properties. */
export function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** Emit only a caller-owned diagnostic, without response bodies or error causes. */
export function refuseRelease(message) {
  const failure = new Error(message);
  failure.name = "ReleaseImageError";
  throw failure;
}

/** Restrict production access to GHCR and unauthenticated explicit loopback fixtures. */
function parseRepository(repository) {
  const component = "[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*";
  const repositoryPattern = new RegExp(`^(ghcr\\.io|(?:127\\.0\\.0\\.1|localhost):([0-9]{1,5}))/(${component}(?:/${component})*)$`);
  const match = repositoryPattern.exec(repository);
  if (match === null || repository.length > 255) refuseRelease("Image repository is invalid.");
  const [, host, port, path] = match;
  const local = host !== "ghcr.io";
  if (local && (Number(port) < 1 || Number(port) > 65535)) refuseRelease("Fixture registry port is invalid.");
  if (!local && !path.includes("/")) refuseRelease("GHCR repository must include its owner.");
  return { repository, path, local, origin: `${local ? "http" : "https"}://${host}` };
}

/** Read bounded HTTP bytes without following redirects or exposing transport details. */
export async function readRegistryResponse(url, headers, maximumBytes = 2 ** 20) {
  try {
    const response = await fetch(url, { headers, redirect: "error", signal: AbortSignal.timeout(10_000) });
    const chunks = [];
    let size = 0;
    if (response.body !== null) {
      const reader = response.body.getReader();
      try {
        while (true) {
          const chunk = await reader.read();
          if (chunk.done) break;
          size += chunk.value.byteLength;
          if (size > maximumBytes) {
            await reader.cancel();
            refuseRelease("Registry response exceeds its byte limit.");
          }
          chunks.push(Buffer.from(chunk.value));
        }
      } finally {
        reader.releaseLock();
      }
    }
    return { status: response.status, headers: response.headers, bytes: Buffer.concat(chunks, size) };
  } catch (error) {
    if (error instanceof Error && error.name === "ReleaseImageError") throw error;
    refuseRelease("Registry transport evidence is unavailable.");
  }
}

/** Require the observed GHCR challenge to identify only this repository's read scope. */
export function validateGhcrChallenge(challenge, repositoryPath) {
  if (typeof challenge !== "string" || !challenge.startsWith("Bearer ")) refuseRelease("GHCR authentication challenge is invalid.");
  const parameters = new Map();
  for (const parameter of challenge.slice("Bearer ".length).split(",")) {
    const match = /^\s*(realm|service|scope)="([^"\r\n]*)"\s*$/.exec(parameter);
    if (match === null || parameters.has(match[1])) refuseRelease("GHCR authentication challenge is invalid.");
    parameters.set(match[1], match[2]);
  }
  if (parameters.size !== 3 || parameters.get("realm") !== "https://ghcr.io/token" ||
    parameters.get("service") !== "ghcr.io" || parameters.get("scope") !== `repository:${repositoryPath}:pull`) {
    refuseRelease("GHCR authentication challenge is invalid.");
  }
}

/** Decode bounded registry JSON with a fixed sanitized refusal on invalid data. */
function parseRegistryDocument(bytes) {
  try {
    const document = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
    if (!isRecord(document)) refuseRelease("Registry JSON evidence is invalid.");
    return document;
  } catch {
    refuseRelease("Registry JSON evidence is invalid.");
  }
}

/** Obtain a read token only from the exact qualified GHCR endpoint. */
async function ghcrReadToken(repositoryPath, challenge) {
  validateGhcrChallenge(challenge, repositoryPath);
  const username = process.env.REGISTRY_USERNAME;
  const password = process.env.REGISTRY_PASSWORD;
  if (typeof username !== "string" || username.length === 0 || /[:\r\n]/.test(username) ||
    typeof password !== "string" || password.length === 0) refuseRelease("GHCR credentials are required.");
  const tokenURL = new URL("https://ghcr.io/token");
  tokenURL.searchParams.set("service", "ghcr.io");
  tokenURL.searchParams.set("scope", `repository:${repositoryPath}:pull`);
  const basic = Buffer.from(`${username}:${password}`).toString("base64");
  const response = await readRegistryResponse(tokenURL, { Authorization: `Basic ${basic}` }, 16 * 1024);
  if (response.status !== 200) refuseRelease("GHCR authentication was refused.");
  const document = parseRegistryDocument(response.bytes);
  if (typeof document.token !== "string" || document.token.length === 0 || document.token.length > 16 * 1024 ||
    !/^[A-Za-z0-9._~+\/-]+={0,2}$/.test(document.token)) refuseRelease("GHCR token evidence is invalid.");
  return document.token;
}

/** Distinguish positively absent manifests from authentication or registry failures. */
export function classifyManifestResponse(response, reference, authenticated) {
  if (response.status === 404) {
    const document = parseRegistryDocument(response.bytes);
    if (!authenticated || !Array.isArray(document.errors) || document.errors.length === 0 ||
      !document.errors.every((error) => isRecord(error) && error.code === "MANIFEST_UNKNOWN")) {
      refuseRelease("Registry manifest absence is unproven.");
    }
    return { kind: "absent" };
  }
  if (response.status !== 200) refuseRelease("Registry manifest lookup was refused.");
  const contentType = response.headers.get("content-type");
  if (contentType === null || !manifestTypes.includes(contentType.split(";")[0].trim())) {
    refuseRelease("Registry manifest media type is invalid.");
  }
  const digest = `sha256:${createHash("sha256").update(response.bytes).digest("hex")}`;
  const reportedDigest = response.headers.get("docker-content-digest");
  if ((reportedDigest !== null && reportedDigest !== digest) || (sha256Pattern.test(reference) && reference !== digest)) {
    refuseRelease("Registry manifest digest does not match its bytes.");
  }
  return { kind: "found", digest, bytes: response.bytes, document: parseRegistryDocument(response.bytes) };
}

/** Resolve manifests using typed evidence and one bounded GHCR read-token lifetime. */
export function createRegistryClient(repositoryText) {
  const repository = parseRepository(repositoryText);
  let authorization = { kind: "none" };
  return {
    repository: repository.repository,
    async lookup(reference) {
      if (!sha256Pattern.test(reference) && !/^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/.test(reference)) {
        refuseRelease("Registry manifest reference is invalid.");
      }
      const headers = { Accept: manifestTypes.join(",") };
      if (authorization.kind === "bearer") headers.Authorization = `Bearer ${authorization.token}`;
      const url = `${repository.origin}/v2/${repository.path}/manifests/${reference}`;
      let response = await readRegistryResponse(url, headers);
      if (response.status === 401 && !repository.local && authorization.kind === "none") {
        const token = await ghcrReadToken(repository.path, response.headers.get("www-authenticate"));
        authorization = { kind: "bearer", token };
        response = await readRegistryResponse(url, { ...headers, Authorization: `Bearer ${token}` });
      }
      return classifyManifestResponse(response, reference, repository.local || authorization.kind === "bearer");
    },
  };
}
