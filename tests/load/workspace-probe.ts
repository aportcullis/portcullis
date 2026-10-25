import assert from "node:assert/strict";
import { readFileSync, realpathSync } from "node:fs";
import { createRequire } from "node:module";
import { isAbsolute, join } from "node:path";
import { z } from "zod";

const probeConfiguration = z.discriminatedUnion("kind", [
  z.strictObject({ kind: z.literal("module") }),
  z.strictObject({
    kind: z.literal("member"),
    name: z.string().min(1),
    directory: z.string().refine(isAbsolute),
    scripts: z.array(z.string().min(1)).min(1),
  }),
  z.strictObject({ kind: z.literal("synthetic"), packageName: z.string().regex(/^[a-z0-9][a-z0-9.-]*$/) }),
]);

/** Checks installed workspace behavior from validated data and the actual copied module. */
function runWorkspaceProbe(): void {
  const configurationPath = process.argv[2];
  assert.ok(typeof configurationPath === "string" && isAbsolute(configurationPath), "The workspace probe configuration must be an absolute path");
  const candidate: unknown = JSON.parse(readFileSync(configurationPath, "utf8"));
  const configuration = probeConfiguration.parse(candidate);
  const memberRequire = createRequire(import.meta.url);
  switch (configuration.kind) {
    case "module": {
      assert.deepEqual(z.object({ valid: z.boolean() }).parse({ valid: false }), { valid: false });
      assert.equal(z.boolean().parse(false), false);
      const manifest: unknown = memberRequire("zod/package.json");
      const { version } = z.object({ version: z.string() }).parse(manifest);
      console.log(JSON.stringify({ version, module: realpathSync(memberRequire.resolve("zod")) }));
      break;
    }
    case "member": {
      const manifest: unknown = JSON.parse(readFileSync("package.json", "utf8"));
      const { name, scripts } = z.object({ name: z.string(), scripts: z.record(z.string(), z.string()) }).parse(manifest);
      assert.equal(name, configuration.name);
      assert.equal(process.cwd(), configuration.directory);
      for (const operation of configuration.scripts) assert.equal(typeof scripts[operation], "string", operation);
      break;
    }
    case "synthetic": {
      const rootRequire = createRequire(join(process.cwd(), "package.json"));
      const installed: unknown = rootRequire(configuration.packageName);
      z.object({ usable: z.literal("reviewed-without-build") }).parse(installed);
      break;
    }
  }
}

runWorkspaceProbe();
