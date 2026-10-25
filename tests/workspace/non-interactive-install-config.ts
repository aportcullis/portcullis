import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { isAbsolute } from "node:path";

/** Supplies package identities and the owned output for one install witness. */
export type InstallWitnessConfiguration = { packageName: string; members: readonly string[]; witnessFile: string };

/** Supplies the actual hook executable, reference input and owned result files. */
export type InstallHookConfiguration = { executable: string; referenceInput: string; processGroupFile: string; statusFile: string };

/** Recognizes an object before a fixture inspects untrusted JSON properties. */
export function isFixtureRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Recognizes an unscoped package identity without path or protocol syntax. */
function isFixturePackageName(value: unknown): value is string {
  return typeof value === "string" && /^[a-z0-9][a-z0-9.-]*$/.test(value);
}

/** Recognizes a member directory contained within the fixture root. */
function isFixtureMemberPath(value: unknown): value is string {
  return typeof value === "string" && /^[a-z0-9][a-z0-9-]*(?:\/[a-z0-9][a-z0-9-]*)*$/.test(value);
}

/** Recognizes distinct owned member directories at the JSON boundary. */
function isFixtureMembers(value: unknown): value is readonly string[] {
  if (!Array.isArray(value)) return false;
  const members: readonly unknown[] = value;
  return members.length > 0 && members.every(isFixtureMemberPath) && new Set(members).size === members.length;
}

/** Recognizes a single owned fixture filename without directory traversal. */
function isFixtureFileName(value: unknown): value is string {
  return typeof value === "string" && /^[a-z0-9][a-z0-9.-]*$/.test(value);
}

/** Reads fixture JSON without granting its values executable source semantics. */
function readFixtureConfiguration(path: string): Record<string, unknown> {
  const parsed: unknown = JSON.parse(readFileSync(path, "utf8"));
  assert.ok(isFixtureRecord(parsed), "Fixture configuration must be an object");
  return parsed;
}

/** Validates the package identities and owned file consumed by the install witness. */
export function readInstallWitnessConfiguration(path: string): InstallWitnessConfiguration {
  const { packageName, members, witnessFile } = readFixtureConfiguration(path);
  assert.ok(isFixturePackageName(packageName), "Fixture package name must be an unscoped package identity");
  assert.ok(isFixtureMembers(members), "Fixture members must be distinct owned relative directories");
  assert.ok(isFixtureFileName(witnessFile), "Fixture witness file must be an owned filename");
  return { packageName, members, witnessFile };
}

/** Validates the hook command and owned output filenames before native execution. */
export function readInstallHookConfiguration(path: string): InstallHookConfiguration {
  const { executable, referenceInput, processGroupFile, statusFile } = readFixtureConfiguration(path);
  assert.ok(typeof executable === "string" && isAbsolute(executable) && !executable.includes("\0"), "Fixture hook executable must be an absolute path");
  assert.ok(typeof referenceInput === "string" && /^refs\/heads\/[^\s]+ [a-f0-9]{40,64} refs\/heads\/[^\s]+ [a-f0-9]{40,64}\n$/.test(referenceInput), "Fixture hook reference input must contain one complete reference update");
  assert.ok(isFixtureFileName(processGroupFile) && isFixtureFileName(statusFile) && processGroupFile !== statusFile, "Fixture hook output files must be distinct owned filenames");
  return { executable, referenceInput, processGroupFile, statusFile };
}
