import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const projectRoot = fileURLToPath(new URL("../..", import.meta.url));
const moduleName = "github.com/aportcullis/portcullis";

/** Copy one actual Make recipe without executing unrelated project setup. */
function readTestRecipe(lines, target) {
  const positions = lines.flatMap((line, index) => line === `${target}:` ? [index] : []);
  assert.equal(positions.length, 1, `Exactly one ${target} target must exist`);
  const recipe = [];
  for (const line of lines.slice(positions[0] + 1)) {
    if (!line.startsWith("\t")) break;
    recipe.push(line);
  }
  assert.equal(recipe.length, 1, `The ${target} target must have one nonempty recipe`);
  return `${target}:\n${recipe.join("\n")}\n`;
}

/** Run a scenario in an owned module using the real pinned dependency graph. */
export async function withPackageFixture(scenario) {
  const directory = mkdtempSync(join(tmpdir(), "portcullis-package-scheduling-"));
  const environment = { ...process.env, GOWORK: "off", GOTOOLCHAIN: "local", GOFLAGS: "-mod=readonly", PORTCULLIS_TEST_DATABASE_REQUIRED: "0", PORTCULLIS_PACKAGE_WITNESSES: join(directory, "witnesses") };
  const context = { directory, environment, processState: { kind: "idle" } };
  try {
    for (const name of ["go.mod", "go.sum"]) copyFileSync(join(projectRoot, name), join(directory, name));
    mkdirSync(join(directory, "witnesses"));
    const lines = readFileSync(join(projectRoot, "Makefile"), "utf8").split("\n");
    const environmentDefinitions = lines.filter((line) => line.startsWith("GO_TEST_ENV :="));
    assert.equal(environmentDefinitions.length, 1, "Exactly one required-database environment definition must exist");
    writeFileSync(join(directory, "Makefile"), `GO_TEST_PACKAGES ?= ./...\n${environmentDefinitions[0]}\n${readTestRecipe(lines, "test")}\n${readTestRecipe(lines, "test-race")}`);
    const runner = ".github/scripts/test-packages.sh";
    if (existsSync(join(projectRoot, runner))) {
      mkdirSync(join(directory, ".github/scripts"), { recursive: true });
      copyFileSync(join(projectRoot, runner), join(directory, runner));
    }
    await scenario(context);
  } finally {
    assert.equal(context.processState.kind, "idle", `Owned processes remain active; fixture retained at ${directory}`);
    rmSync(directory, { recursive: true, force: true });
    assert.equal(existsSync(directory), false, "Owned package fixture must be removed");
  }
}

/** Emit an observable test package without invoking Docker or replacing Go. */
export function writeWitnessPackage(context, name, behavior = "pass", buildTag = "") {
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
export function writePackageWithoutTests(context, name) {
  mkdirSync(join(context.directory, name));
  writeFileSync(join(context.directory, name, "package.go"), "package fixture\n");
}

/** Read actual executions and verify uncached shuffled required-database admission. */
export function readPackageWitnesses(context, name) {
  const path = join(context.directory, "witnesses", `${name}.jsonl`);
  if (!existsSync(path)) return [];
  return readFileSync(path, "utf8").trim().split("\n").map((line) => {
    const witness = JSON.parse(line);
    assert.equal(witness.package, name);
    assert.ok(Number.isSafeInteger(witness.process) && witness.process > 1);
    assert.ok(Number.isSafeInteger(witness.parent) && witness.parent > 1);
    assert.ok(Number.isSafeInteger(witness.group) && witness.group > 1);
    assert.equal(typeof witness.session, "string");
    assert.notEqual(witness.session, "");
    assert.equal(witness.count, "1");
    assert.equal(witness.shuffle, "on");
    return witness;
  });
}

/** Assert exactly the requested successful package executions occurred. */
export function assertPackageTargetSucceeded(context, result, names) {
  assert.equal(result.status, 0, result.output);
  return names.map((name) => {
    const witnesses = readPackageWitnesses(context, name);
    assert.equal(witnesses.length, 1, `Expected exactly one execution of ${moduleName}/${name}`);
    return witnesses[0];
  });
}

/** Determine whether this invocation's exact owned group still exists. */
function ownedGroupExists(processGroup) {
  assert.ok(Number.isSafeInteger(processGroup) && processGroup > 1 && processGroup !== process.pid, "Only an exact owned process group may be probed");
  try { process.kill(-processGroup, 0); return true; } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
    return false;
  }
}

/** Require natural group exit before any fallback termination can hide a failure. */
async function requireOwnedGroupAbsent(processGroup) {
  const deadline = Date.now() + 3000;
  while (ownedGroupExists(processGroup)) {
    assert.ok(Date.now() < deadline, "The exact owned test group did not exit");
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
}

/** Bound stream closure even when a descendant retains the command's pipes. */
async function requireMakeStreamsClosed(closed) {
  const expiration = Promise.withResolvers();
  const timer = setTimeout(() => expiration.resolve({ kind: "deadline" }), 3000);
  try {
    const result = await Promise.race([closed, expiration.promise]);
    assert.equal(result.kind, "closed", "Owned command streams did not close after process termination");
  } finally {
    clearTimeout(timer);
  }
}

/** Execute one real Make job with a deadline and one strictly owned process group. */
async function executePackageTarget(context, selection, target, action) {
  assert.equal(context.processState.kind, "idle", "Fixture commands must run sequentially");
  const manifests = ["go.mod", "go.sum"].map((name) => readFileSync(join(context.directory, name)));
  const child = spawn("make", [target, `GO_TEST_PACKAGES=${selection}`], { cwd: context.directory, env: context.environment, detached: true, stdio: "pipe" });
  const finished = Promise.withResolvers();
  const closed = Promise.withResolvers();
  const expiration = Promise.withResolvers();
  let output = "";
  let commandState = { kind: "running" };
  let deadlineFired = false;
  let activeProcess = { kind: "not-ready" };
  const receive = (chunk) => { output += chunk.toString("utf8"); };
  child.stdout.on("data", receive);
  child.stderr.on("data", receive);
  child.once("error", (error) => {
    commandState = { kind: "failed", error };
    finished.resolve(commandState);
  });
  child.once("exit", (status, signal) => {
    commandState = { kind: "exited", status, signal };
    finished.resolve(commandState);
  });
  child.once("close", () => closed.resolve({ kind: "closed" }));
  if (typeof child.pid !== "number") {
    const result = await finished.promise;
    await requireMakeStreamsClosed(closed.promise);
    if (result.kind === "failed") throw result.error;
    assert.fail("The owned Make process did not start");
  }
  const processGroup = child.pid;
  assert.ok(Number.isSafeInteger(processGroup) && processGroup > 1 && processGroup !== process.pid);
  context.processState = { kind: "running", processGroup };
  const expire = () => { deadlineFired = true; expiration.resolve({ kind: "deadline" }); };
  let timer = setTimeout(expire, 120000);
  const createDeadlineError = () => new Error("The owned Make target exceeded its fixture deadline");
  try {
    if (action.kind !== "complete") {
      const ready = join(context.directory, "witnesses/active.ready");
      while (!existsSync(ready)) {
        if (deadlineFired) throw createDeadlineError();
        if (commandState.kind === "failed") throw commandState.error;
        assert.equal(commandState.kind, "running", `The active package exited before readiness:\n${output}`);
        await Promise.race([new Promise((resolve) => setTimeout(resolve, 25)), finished.promise, expiration.promise]);
      }
      const active = readPackageWitnesses(context, "active");
      assert.equal(active.length, 1, "Exactly one real active binary must become ready");
      assert.equal(active[0].group, processGroup, "The active binary must belong to the exact owned Make job");
      activeProcess = { kind: "ready", process: active[0].process };
      clearTimeout(timer);
      if (action.kind === "signal") {
        timer = setTimeout(expire, 5000);
        process.kill(-processGroup, action.signal);
      } else {
        assert.equal(action.kind, "deadline-after-ready");
        assert.ok(Number.isSafeInteger(action.milliseconds) && action.milliseconds > 0);
        timer = setTimeout(expire, action.milliseconds);
      }
    }
    const result = await Promise.race([finished.promise, expiration.promise]);
    if (result.kind === "deadline" || deadlineFired) throw createDeadlineError();
    if (result.kind === "failed") throw result.error;
    await requireOwnedGroupAbsent(processGroup);
    await requireMakeStreamsClosed(closed.promise);
    if (deadlineFired) throw createDeadlineError();
    if (activeProcess.kind === "ready") assert.throws(() => process.kill(activeProcess.process, 0), { code: "ESRCH" }, "The active binary must exit without fallback termination");
    return { status: result.status, signal: result.signal, output };
  } finally {
    clearTimeout(timer);
    child.stdin.destroy();
    try {
      if (ownedGroupExists(processGroup)) process.kill(-processGroup, "SIGKILL");
    } finally {
      await requireMakeStreamsClosed(closed.promise);
    }
    await requireOwnedGroupAbsent(processGroup);
    context.processState = { kind: "idle" };
    assert.deepEqual(["go.mod", "go.sum"].map((name) => readFileSync(join(context.directory, name))), manifests, "Execution must not change pinned dependencies");
  }
}

/** Run the actual Make recipe and retain its observable status and output. */
export async function runPackageTarget(context, selection = "./...", target = "test", action = { kind: "complete" }) {
  return executePackageTarget(context, selection, target, action);
}

/** Require requested terminal cancellation before any fallback cleanup occurs. */
export async function cancelReadyPackageTarget(context, signal) {
  const result = await executePackageTarget(context, "./active ./later", "test", { kind: "signal", signal });
  assert.notEqual(result.status, 0, "Terminal cancellation must fail the Make target");
  assert.equal(readPackageWitnesses(context, "later").length, 0, "Cancellation must stop later package admission");
  return result;
}
