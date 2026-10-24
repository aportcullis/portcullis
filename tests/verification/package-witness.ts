import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

/** Records one actual Go test process and its required execution settings. */
export type PackageWitness = {
  package: string;
  process: number;
  parent: number;
  group: number;
  session: string;
  count: "1";
  shuffle: "on";
};

/** Recognizes a non-array JSON object without trusting its property values. */
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Recognizes a process identifier safe for an explicitly owned process probe. */
function isProcessIdentifier(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 1;
}

/** Recognizes the complete witness written by the real Go scenario. */
function isPackageWitness(value: unknown): value is PackageWitness {
  if (!isRecord(value)) return false;
  return typeof value.package === "string"
    && isProcessIdentifier(value.process)
    && isProcessIdentifier(value.parent)
    && isProcessIdentifier(value.group)
    && typeof value.session === "string" && value.session !== ""
    && value.count === "1" && value.shuffle === "on";
}

/** Refuses malformed observations before they become process identifiers or results. */
function assertPackageWitness(value: unknown): asserts value is PackageWitness {
  assert.ok(isPackageWitness(value), "The real Go package returned an invalid execution witness");
}

/** Reads actual executions, preserving absence as an empty execution history. */
export function readPackageWitnessFile(directory: string, name: string): PackageWitness[] {
  const path = join(directory, "witnesses", `${name}.jsonl`);
  if (!existsSync(path)) return [];
  const contents = readFileSync(path, "utf8").trim();
  assert.notEqual(contents, "", "An existing witness file must contain a completed observation");
  return contents.split("\n").map((line) => {
    const parsed: unknown = JSON.parse(line);
    assertPackageWitness(parsed);
    assert.equal(parsed.package, name);
    return parsed;
  });
}
