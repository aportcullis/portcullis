import assert from "node:assert/strict";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { readPackageWitnessFile, type PackageWitness } from "#verification/package-witness";
import { executeOwnedVerification, type VerificationAction, type VerificationContext, type VerificationResult } from "#verification/verification-process";

const projectRoot = fileURLToPath(new URL("../..", import.meta.url));
const moduleName = "github.com/aportcullis/portcullis";
type PackageBehavior = "pass" | "fail" | "compile-error" | "wait";
type PackageTarget = "test" | "test-race";
type PackageFixtureFailure = { kind: "none" } | { kind: "failed"; error: unknown };

/** Copies one actual Make recipe without executing unrelated project setup. */
function readTestRecipe(lines: readonly string[], target: PackageTarget): string {
  const positions = lines.flatMap((line, index) => line === `${target}:` ? [index] : []);
  const [position] = positions;
  assert.equal(positions.length, 1, `Exactly one ${target} target must exist`);
  assert.ok(position !== undefined, `The ${target} target must be present`);
  const recipe: string[] = [];
  for (const line of lines.slice(position + 1)) {
    if (!line.startsWith("\t")) break;
    recipe.push(line);
  }
  assert.equal(recipe.length, 1, `The ${target} target must have one nonempty recipe`);
  return `${target}:\n${recipe.join("\n")}\n`;
}

/** Runs the pinned module scenario while preserving primary failures and disposal refusals. */
export async function withPackageFixture(scenario: (context: VerificationContext) => Promise<void>): Promise<void> {
  const directory = mkdtempSync(join(tmpdir(), "portcullis-package-scheduling-"));
  const environment: NodeJS.ProcessEnv = { ...process.env, GOWORK: "off", GOTOOLCHAIN: "local", GOFLAGS: "-mod=readonly", PORTCULLIS_TEST_DATABASE_REQUIRED: "0", PORTCULLIS_PACKAGE_WITNESSES: join(directory, "witnesses") };
  const context: VerificationContext = { directory, environment, processState: { kind: "idle" } };
  let failure: PackageFixtureFailure = { kind: "none" };
  try {
    for (const name of ["go.mod", "go.sum"]) copyFileSync(join(projectRoot, name), join(directory, name));
    mkdirSync(join(directory, "witnesses"));
    const lines = readFileSync(join(projectRoot, "Makefile"), "utf8").split("\n");
    const environmentDefinitions = lines.filter((line) => line.startsWith("GO_TEST_ENV :="));
    const [environmentDefinition] = environmentDefinitions;
    assert.equal(environmentDefinitions.length, 1, "Exactly one required-database environment definition must exist");
    assert.ok(environmentDefinition !== undefined, "The required-database environment must be defined");
    writeFileSync(join(directory, "Makefile"), `GO_TEST_PACKAGES ?= ./...\n${environmentDefinition}\n${readTestRecipe(lines, "test")}\n${readTestRecipe(lines, "test-race")}`);
    const runner = ".github/scripts/test-packages.sh";
    if (existsSync(join(projectRoot, runner))) {
      mkdirSync(join(directory, ".github/scripts"), { recursive: true });
      copyFileSync(join(projectRoot, runner), join(directory, runner));
    }
    await scenario(context);
  } catch (error: unknown) {
    failure = { kind: "failed", error };
  }
  try {
    assert.equal(context.processState.kind, "idle", `Owned processes remain active; fixture retained at ${directory}`);
    rmSync(directory, { recursive: true, force: true });
    assert.equal(existsSync(directory), false, "Owned package fixture must be removed");
  } catch (error: unknown) {
    if (failure.kind === "failed") throw new AggregateError([failure.error, error], "Package fixture execution and disposal both failed");
    throw error;
  }
  if (failure.kind === "failed") throw failure.error;
}

/** Emit an observable test package without invoking Docker or replacing Go. */
export function writeWitnessPackage(context: VerificationContext, name: string, behavior: PackageBehavior = "pass", buildTag: "" | "race" = ""): void {
  const directory = join(context.directory, name);
  mkdirSync(directory);
  const prefix = buildTag === "" ? "" : `//go:build ${buildTag}\n\n`;
  writeFileSync(join(directory, "package.go"), `${prefix}package fixture\n`);
  writeFileSync(join(directory, "package_test.go"), `${prefix}package fixture

import (
  "encoding/json"
  "flag"
  "os"
  "path/filepath"
  "syscall"
  "testing"
  ${behavior === "wait" ? '"time"' : ""}

  "github.com/testcontainers/testcontainers-go"
)

// TestPackageWitness reports the real package process and execution contract.
func TestPackageWitness(t *testing.T) {
  count := flag.Lookup("test.count")
  shuffle := flag.Lookup("test.shuffle")
  if count == nil || shuffle == nil { t.Fatal("required testing flags are unavailable") }
  if os.Getenv("PORTCULLIS_TEST_DATABASE_REQUIRED") != "1" { t.Fatal("database requirement was not propagated") }
  witness := struct {
    Package string \`json:"package"\`
    Process int \`json:"process"\`
    Parent int \`json:"parent"\`
    Group int \`json:"group"\`
    Session string \`json:"session"\`
    Count string \`json:"count"\`
    Shuffle string \`json:"shuffle"\`
  }{${JSON.stringify(name)}, os.Getpid(), os.Getppid(), syscall.Getpgrp(), testcontainers.SessionID(), count.Value.String(), shuffle.Value.String()}
  output, err := os.OpenFile(filepath.Join(os.Getenv("PORTCULLIS_PACKAGE_WITNESSES"), ${JSON.stringify(`${name}.jsonl`)}), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
  if err != nil { t.Fatal(err) }
  if err := json.NewEncoder(output).Encode(witness); err != nil { t.Fatal(err) }
  if err := output.Close(); err != nil { t.Fatal(err) }
  if err := os.WriteFile(filepath.Join(os.Getenv("PORTCULLIS_PACKAGE_WITNESSES"), ${JSON.stringify(`${name}.ready`)}), []byte("ready"), 0600); err != nil { t.Fatal(err) }
  ${behavior === "compile-error" ? "missingPackageSchedulingSymbol()" : ""}
  ${behavior === "fail" ? 't.Fatal("intentional package assertion refusal")' : ""}
  ${behavior === "wait" ? '<-time.After(time.Minute); t.Fatal("the owning process group did not cancel the waiting fixture")' : ""}
}
`);
}

/** Add a normal selected package that has no test binary to execute. */
export function writePackageWithoutTests(context: VerificationContext, name: string): void {
  mkdirSync(join(context.directory, name));
  writeFileSync(join(context.directory, name, "package.go"), "package fixture\n");
}

/** Reads actual executions and verifies their required process contract. */
export function readPackageWitnesses(context: VerificationContext, name: string): PackageWitness[] {
  return readPackageWitnessFile(context.directory, name);
}

/** Asserts exactly the requested successful package executions occurred. */
export function assertPackageTargetSucceeded(context: VerificationContext, result: VerificationResult, names: readonly string[]): PackageWitness[] {
  assert.equal(result.status, 0, result.output);
  return names.map((name) => {
    const observations = readPackageWitnesses(context, name);
    const [observation] = observations;
    assert.equal(observations.length, 1, `Expected exactly one execution of ${moduleName}/${name}`);
    assert.ok(observation !== undefined, `The ${name} package must provide its execution observation`);
    return observation;
  });
}

/** Runs the actual Make recipe while preserving fixture dependency manifests. */
export async function runPackageTarget(context: VerificationContext, selection = "./...", target: PackageTarget = "test", action: VerificationAction = { kind: "complete" }): Promise<VerificationResult> {
  const manifests = ["go.mod", "go.sum"].map((name) => readFileSync(join(context.directory, name)));
  try {
    return await executeOwnedVerification(context, "make", [target, `GO_TEST_PACKAGES=${selection}`], action);
  } finally {
    assert.deepEqual(["go.mod", "go.sum"].map((name) => readFileSync(join(context.directory, name))), manifests, "Execution must not change pinned dependencies");
  }
}

/** Requires requested terminal cancellation before any fallback cleanup occurs. */
export async function cancelReadyPackageTarget(context: VerificationContext, signal: "SIGINT" | "SIGTERM"): Promise<VerificationResult> {
  const result = await runPackageTarget(context, "./active ./later", "test", { kind: "signal", signal });
  assert.notEqual(result.status, 0, "Terminal cancellation must fail the Make target");
  assert.equal(readPackageWitnesses(context, "later").length, 0, "Cancellation must stop later package admission");
  return result;
}
