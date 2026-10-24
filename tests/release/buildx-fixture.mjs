import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { access, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { command } from "./promotion-fixture.mjs";

const buildkitImage = "moby/buildkit:v0.34.1@sha256:99368f3e46177c6405b9740e4461d91cee004128daeecca78d4ceac50ec1da5c";

/** Distinguish an exact owned Docker resource's absence from daemon failure. */
async function ownedResourcePresent(kind, name) {
  try {
    await command("docker", [kind, "inspect", name]);
    return true;
  } catch (error) {
    assert.ok(error instanceof Error && "stderr" in error);
    const diagnostic = String(error.stderr);
    const absent = kind === "container" ? diagnostic.includes(`No such container: ${name}`) :
      diagnostic.includes(`get ${name}: no such volume`);
    assert.ok(absent, `owned ${kind} absence is unproven: ${name}`);
    return false;
  }
}

/** Construct a private builder lifetime without selecting or altering a default builder. */
export function ownedBuildxBuilder(directory, registry) {
  const name = `pc-promotion-${randomUUID()}`;
  const configurationDirectory = join(directory, "buildx");
  const instancePath = join(configurationDirectory, "instances", name);
  const daemonConfiguration = join(directory, "buildkitd.toml");
  const environment = { ...process.env, BUILDX_CONFIG: configurationDirectory };
  const container = `buildx_buildkit_${name}0`;
  const volume = `${container}_state`;
  return {
    name,
    environment,
    async start(signal) {
      await mkdir(configurationDirectory);
      await writeFile(daemonConfiguration, `[registry."${registry.repositoryHost}"]\n  http = true\n`);
      await command("docker", ["buildx", "create", "--name", name, "--driver", "docker-container",
        "--driver-opt", `image=${buildkitImage}`, "--driver-opt", `network=container:${registry.container}`,
        "--buildkitd-config", daemonConfiguration, "--bootstrap"], { env: environment, signal, timeout: 90_000 });
    },
    async close() {
      let instancePresent;
      try {
        await access(instancePath);
        instancePresent = true;
      } catch (error) {
        assert.ok(error instanceof Error && "code" in error && error.code === "ENOENT");
        instancePresent = false;
      }
      if (instancePresent) await command("docker", ["buildx", "rm", "--force", name], { env: environment });
      if (await ownedResourcePresent("container", container)) await command("docker", ["container", "rm", "--force", container]);
      assert.equal(await ownedResourcePresent("container", container), false);
      if (await ownedResourcePresent("volume", volume)) await command("docker", ["volume", "rm", "--force", volume]);
      assert.equal(await ownedResourcePresent("volume", volume), false);
      await assert.rejects(access(instancePath), (error) => error instanceof Error && "code" in error && error.code === "ENOENT");
    },
  };
}
