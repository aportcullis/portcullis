import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { createServer } from "node:http";
import test from "node:test";

import { createRegistryClient, manifestTypes, readRegistryResponse, validateGhcrChallenge } from "../../.github/scripts/release-registry.mjs";

/** Own one bounded HTTP evidence source without substituting Docker command results. */
async function evidenceServer(scenario, handler) {
  const server = createServer(handler);
  scenario.after(async () => {
    server.closeAllConnections();
    await new Promise((resolveClose, rejectClose) => server.close((error) => {
      if (error) rejectClose(error);
      else resolveClose();
    }));
    assert.equal(server.listening, false);
  });
  await new Promise((resolveListen, rejectListen) => {
    server.once("error", rejectListen);
    server.listen(0, "127.0.0.1", resolveListen);
  });
  const address = server.address();
  assert.ok(address !== null && typeof address === "object");
  return { origin: `http://127.0.0.1:${address.port}`, repository: `127.0.0.1:${address.port}/fixture` };
}

/** Supply protocol bytes with an independently calculated identity. */
async function documentServer(scenario, status, document, mediaType = "application/json", headers = {}) {
  const bytes = Buffer.from(typeof document === "string" ? document : JSON.stringify(document));
  const digest = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
  const server = await evidenceServer(scenario, (_request, response) => {
    response.writeHead(status, { "Content-Type": mediaType, ...headers });
    response.end(bytes);
  });
  return { ...server, bytes, digest };
}

/** Assert the public diagnostic excludes opaque HTTP content and error causes. */
function sanitizedRefusal(error) {
  assert.ok(error instanceof Error);
  assert.equal(error.name, "ReleaseImageError");
  assert.doesNotMatch(error.message, /private-response-sentinel/);
  assert.equal("cause" in error, false);
  return true;
}

for (const mediaType of manifestTypes) {
  test(`lookup retains existing ${mediaType}`, async (scenario) => {
    const server = await documentServer(scenario, 200, { schemaVersion: 2, mediaType }, mediaType);
    const result = await createRegistryClient(server.repository).lookup("1.2.3");
    assert.equal(result.kind, "found");
    assert.equal(result.digest, server.digest);
    assert.deepEqual(result.bytes, server.bytes);
  });
}

test("only positively classified manifest absence admits a new version", async (scenario) => {
  const server = await documentServer(scenario, 404, { errors: [{ code: "MANIFEST_UNKNOWN" }] });
  assert.deepEqual(await createRegistryClient(server.repository).lookup("1.2.3"), { kind: "absent" });
});

test("the observed exact GHCR read challenge is admitted", async (scenario) => {
  const challenge = 'Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:aportcullis/portcullis:pull"';
  const server = await documentServer(scenario, 401, {}, "application/json", { "WWW-Authenticate": challenge });
  const response = await readRegistryResponse(`${server.origin}/v2/`, {});
  assert.doesNotThrow(() => validateGhcrChallenge(response.headers.get("www-authenticate"), "aportcullis/portcullis"));
});

for (const errors of [[], [{ code: "NAME_UNKNOWN" }], [{ code: "MANIFEST_UNKNOWN" }, { code: "DENIED" }],
  [{ code: "MANIFEST_UNKNOWN" }, { code: "UNAUTHORIZED" }], [null]]) {
  test(`ambiguous 404 refuses ${JSON.stringify(errors)}`, async (scenario) => {
    const server = await documentServer(scenario, 404, { errors, private: "private-response-sentinel" });
    await assert.rejects(createRegistryClient(server.repository).lookup("1.2.3"), sanitizedRefusal);
  });
}

for (const status of [401, 403, 429, 500, 503]) {
  test(`HTTP ${status} cannot mean a missing full version`, async (scenario) => {
    const server = await documentServer(scenario, status, { errors: [{ code: "MANIFEST_UNKNOWN" }], private: "private-response-sentinel" });
    await assert.rejects(createRegistryClient(server.repository).lookup("1.2.3"), sanitizedRefusal);
  });
}

test("malformed absence JSON refuses without disclosing its bytes", async (scenario) => {
  const server = await documentServer(scenario, 404, "private-response-sentinel: not JSON");
  await assert.rejects(createRegistryClient(server.repository).lookup("1.2.3"), sanitizedRefusal);
});

test("invalid UTF-8 cannot become valid manifest absence", async (scenario) => {
  const bytes = Buffer.concat([Buffer.from('{"errors":[{"code":"MANIFEST_UNKNOWN","message":"'),
    Buffer.from([0xff]), Buffer.from('"}]}')]);
  const server = await evidenceServer(scenario, (_request, response) => {
    response.writeHead(404, { "Content-Type": "application/json" });
    response.end(bytes);
  });
  await assert.rejects(createRegistryClient(server.repository).lookup("1.2.3"), sanitizedRefusal);
});

test("a mismatched manifest digest header refuses", async (scenario) => {
  const server = await documentServer(scenario, 200, { schemaVersion: 2 }, manifestTypes[0],
    { "Docker-Content-Digest": `sha256:${"0".repeat(64)}` });
  await assert.rejects(createRegistryClient(server.repository).lookup("1.2.3"), sanitizedRefusal);
});

test("content fetched by a digest must hash to that exact digest", async (scenario) => {
  const server = await documentServer(scenario, 200, { schemaVersion: 2 }, manifestTypes[0]);
  await assert.rejects(createRegistryClient(server.repository).lookup(`sha256:${"1".repeat(64)}`), sanitizedRefusal);
});

test("redirects never forward even fixture bearer credentials", async (scenario) => {
  let forwardedRequests = 0;
  const destination = await evidenceServer(scenario, (_request, response) => {
    forwardedRequests++;
    response.end("private-response-sentinel");
  });
  const redirect = await evidenceServer(scenario, (_request, response) => {
    response.writeHead(302, { Location: `${destination.origin}/capture` });
    response.end();
  });
  await assert.rejects(readRegistryResponse(`${redirect.origin}/v2/`, { Authorization: "Bearer fixture-credential" }), sanitizedRefusal);
  assert.equal(forwardedRequests, 0);
});

test("registry responses have a real streamed byte bound", async (scenario) => {
  const server = await evidenceServer(scenario, (_request, response) => {
    response.writeHead(200, { "Content-Type": manifestTypes[0] });
    response.end(Buffer.alloc(2 ** 20 + 1, 0x20));
  });
  await assert.rejects(createRegistryClient(server.repository).lookup("1.2.3"), sanitizedRefusal);
});

for (const challenge of [
  'Bearer realm="https://evil.invalid/token",service="ghcr.io",scope="repository:aportcullis/portcullis:pull"',
  'Bearer realm="https://ghcr.io/other",service="ghcr.io",scope="repository:aportcullis/portcullis:pull"',
  'Bearer realm="https://ghcr.io/token",service="other",scope="repository:aportcullis/portcullis:pull"',
  'Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:other/private:pull"',
  'Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:aportcullis/portcullis:push"',
  'Bearer realm="https://ghcr.io/token",realm="https://evil.invalid/token",service="ghcr.io",scope="repository:aportcullis/portcullis:pull"',
]) {
  test(`unqualified GHCR challenge refuses ${challenge}`, async (scenario) => {
    const server = await documentServer(scenario, 401, {}, "application/json", { "WWW-Authenticate": challenge });
    const response = await readRegistryResponse(`${server.origin}/v2/`, {});
    assert.throws(() => validateGhcrChallenge(response.headers.get("www-authenticate"), "aportcullis/portcullis"), sanitizedRefusal);
  });
}

test("canonical double-underscore and repeated-dash repositories remain usable", () => {
  assert.doesNotThrow(() => createRegistryClient("ghcr.io/fixture/portcullis__fork--review"));
});
