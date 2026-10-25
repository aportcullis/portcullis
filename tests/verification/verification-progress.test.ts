import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { requireOwnedVerificationGroupAbsent, terminateOwnedVerificationGroup } from "#verification/verification-process";

const projectRoot = fileURLToPath(new URL("../..", import.meta.url));
type ProgressLogMode = "standard" | "spaced" | "unwritable";
type ProgressOptions = { behavior: "pass" | "fail"; hold: boolean; logMode: ProgressLogMode };
type ProgressResult = { kind: "exited"; status: number } | { kind: "signalled"; signal: NodeJS.Signals };
type ProgressFixture = { directory: string; log: string; output: () => string; closed: Promise<ProgressResult>; isRunning: () => boolean; group: number };
type ProgressFailure = { kind: "none" } | { kind: "failed"; error: unknown };
type ProgressOwnership = { kind: "not-started" } | { kind: "owned"; group: number; directory: string };

async function awaitProgressObservation<Value>(observation: Promise<Value>): Promise<Value> {
  const expiration = Promise.withResolvers<never>();
  const timer = setTimeout(() => expiration.reject(new Error("The verification progress observation did not arrive")), 3000);
  try { return await Promise.race([observation, expiration.promise]); }
  finally { clearTimeout(timer); }
}

async function awaitProgressCondition(condition: () => boolean): Promise<void> {
  const observation = Promise.withResolvers<void>();
  const timer = setInterval(() => { if (condition()) observation.resolve(); }, 10);
  try { await awaitProgressObservation(observation.promise); }
  finally { clearInterval(timer); }
}

/** Runs the actual group recipe against a maintained Node fixture, without Go or Docker. */
async function withProgressFixture({ behavior, hold, logMode }: ProgressOptions, scenario: (fixture: ProgressFixture) => Promise<void>): Promise<void> {
  const directory = mkdtempSync(join(tmpdir(), "portcullis-verification-progress-"));
  const logs = join(directory, logMode === "spaced" ? "group logs" : "logs");
  mkdirSync(logs);
  const log = join(logs, "progress.log");
  if (logMode === "unwritable") mkdirSync(log);
  copyFileSync(join(projectRoot, "tests/verification/verification-progress.mk"), join(directory, "Makefile"));
  copyFileSync(join(projectRoot, "Makefile"), join(directory, "project.mk"));
  const child = spawn("make", ["--no-print-directory", "verify-group-progress", `VERIFY_LOG_DIR=${logs}`], {
    cwd: directory, detached: true, stdio: ["ignore", "pipe", "pipe"],
    env: {
      ...process.env, VERIFICATION_NODE: process.execPath,
      VERIFICATION_PROGRESS_CHILD: join(projectRoot, "tests/verification/verification-progress-child.ts"),
      VERIFICATION_PROGRESS_DIRECTORY: directory, VERIFICATION_PROGRESS_BEHAVIOR: behavior,
      VERIFICATION_PROGRESS_HOLD: String(hold), GIT_CONFIG_COUNT: "1", GIT_CONFIG_KEY_0: "core.hooksPath", GIT_CONFIG_VALUE_0: join(directory, "hooks"),
    },
  });
  const group = child.pid;
  assert.ok(typeof group === "number" && group > 1 && group !== process.pid);
  let output = "";
  let running = true;
  const receiveOutput = (chunk: Buffer): void => { output += chunk.toString("utf8"); };
  child.stdout.on("data", receiveOutput);
  child.stderr.on("data", receiveOutput);
  const closed = new Promise<ProgressResult>((resolve, reject) => {
    child.once("error", reject);
    child.once("close", (status, signal) => {
      running = false;
      if (typeof status === "number") { resolve({ kind: "exited", status }); return; }
      if (typeof signal === "string") { resolve({ kind: "signalled", signal }); return; }
      reject(new Error("The progress command closed without an exit status or signal"));
    });
  });
  let failure: ProgressFailure = { kind: "none" };
  try {
    await scenario({ directory, log, output: () => output, closed, isRunning: () => running, group });
  } catch (error: unknown) {
    failure = { kind: "failed", error };
    try {
      const diagnostics = [`Output:\n${output}`];
      if (existsSync(log) && logMode !== "unwritable") diagnostics.push(`Log:\n${readFileSync(log, "utf8")}`);
      failure = { kind: "failed", error: new AggregateError([error], `Progress observation failed.\n${diagnostics.join("\n")}`) };
    } catch (diagnosticError: unknown) {
      failure = { kind: "failed", error: new AggregateError([error, diagnosticError], "Progress observation and diagnostic read both failed") };
    }
  }
  try {
    if (running) {
      try { process.kill(-group, "SIGTERM"); }
      catch (error: unknown) { if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error; }
    }
    try { await awaitProgressObservation(closed); }
    catch (error: unknown) { terminateOwnedVerificationGroup(group); await awaitProgressObservation(closed); throw error; }
    await requireOwnedVerificationGroupAbsent(group);
    rmSync(directory, { recursive: true, force: true });
  } catch (error: unknown) {
    if (failure.kind === "failed") throw new AggregateError([failure.error, error], "Progress observation and owned cleanup both failed");
    throw error;
  }
  if (failure.kind === "failed") throw failure.error;
}

for (const stream of ["stdout", "stderr"]) {
  test(`the group exposes ${stream} before its active command completes`, async () => {
    await withProgressFixture({ behavior: "pass", hold: true, logMode: "standard" }, async ({ directory, log, output, closed, isRunning }) => {
      await awaitProgressCondition(() => existsSync(join(directory, "ready")));
      assert.equal(isRunning(), true);
      await awaitProgressCondition(() => output().includes(`fixture ${stream} before completion`));
      assert.match(output(), /verify-progress started.*log/);
      writeFileSync(join(directory, "release"), "release");
      assert.deepEqual(await awaitProgressObservation(closed), { kind: "exited", status: 0 }, output());
      const logOutput = readFileSync(log, "utf8");
      assert.match(logOutput, /fixture stdout before completion/);
      assert.match(logOutput, /fixture stderr before completion/);
      assert.equal(output().split(`fixture ${stream} before completion`).length - 1, 1);
      assert.match(output(), /verify-progress passed in \d+s/);
    });
  });
}

test("a successful group retains its full log when the directory contains spaces", async () => {
  await withProgressFixture({ behavior: "pass", hold: false, logMode: "spaced" }, async ({ closed, log, output }) => {
    assert.deepEqual(await awaitProgressObservation(closed), { kind: "exited", status: 0 }, output());
    assert.match(readFileSync(log, "utf8"), /fixture completed/);
    assert.ok(output().includes(log));
  });
});

test("a command failure remains failed while its actual output is emitted once", async () => {
  await withProgressFixture({ behavior: "fail", hold: false, logMode: "standard" }, async ({ closed, log, output }) => {
    const completion = await awaitProgressObservation(closed);
    assert.ok(completion.kind === "exited" && completion.status !== 0);
    assert.match(output(), /verify-progress FAILED in \d+s/);
    assert.equal(output().split("fixture completed").length - 1, 1);
    assert.match(readFileSync(log, "utf8"), /fixture completed/);
  });
});

test("a log write failure cannot turn a successful command into a passing group", async () => {
  await withProgressFixture({ behavior: "pass", hold: false, logMode: "unwritable" }, async ({ closed, output }) => {
    const completion = await awaitProgressObservation(closed);
    assert.ok(completion.kind === "exited" && completion.status !== 0);
    assert.doesNotMatch(output(), /verify-progress passed/);
  });
});

test("a diagnostic read refusal preserves the held scenario failure and cleans its owned group", async () => {
  const ownership: { current: ProgressOwnership } = { current: { kind: "not-started" } };
  const scenarioFailure = new Error("Controlled held progress failure");
  const result = await withProgressFixture({ behavior: "pass", hold: true, logMode: "standard" }, async ({ directory, log, group }) => {
    ownership.current = { kind: "owned", group, directory };
    await awaitProgressCondition(() => existsSync(join(directory, "ready")) && existsSync(log));
    rmSync(log);
    mkdirSync(log);
    throw scenarioFailure;
  }).then<ProgressFailure, ProgressFailure>(
    () => ({ kind: "none" }),
    (error: unknown) => ({ kind: "failed", error }),
  );
  const { current: owned } = ownership;
  if (owned.kind === "not-started") {
    if (result.kind === "failed") throw result.error;
    assert.fail("The held fixture must record its owned group");
  }
  const { group, directory } = owned;
  const cleanupObservation = await requireOwnedVerificationGroupAbsent(group).then<ProgressFailure, ProgressFailure>(
    () => ({ kind: "none" }),
    (error: unknown) => ({ kind: "failed", error }),
  );
  try {
    terminateOwnedVerificationGroup(group);
    await requireOwnedVerificationGroupAbsent(group);
    rmSync(directory, { recursive: true, force: true });
  } catch (error: unknown) {
    throw new AggregateError(result.kind === "failed" ? [result.error, error] : [error], "The diagnostic refusal fixture could not be disposed");
  }
  assert.equal(cleanupObservation.kind, "none", "The held group must be absent before fallback disposal");
  assert.ok(result.kind === "failed");
  const { error } = result;
  assert.ok(error instanceof AggregateError);
  const causes: readonly unknown[] = error.errors;
  assert.ok(causes.includes(scenarioFailure), "The original scenario failure must be preserved");
  assert.ok(causes.some(cause => cause instanceof Error && "code" in cause && cause.code === "EISDIR"), "The diagnostic read refusal must also be preserved");
});

const cancellationSignals: readonly ("SIGINT" | "SIGTERM")[] = ["SIGINT", "SIGTERM"];
for (const signal of cancellationSignals) {
  test(`${signal} cancels the active group without printing success or leaving its child`, async () => {
    await withProgressFixture({ behavior: "pass", hold: true, logMode: "standard" }, async ({ directory, group, closed, output }) => {
      await awaitProgressCondition(() => existsSync(join(directory, "ready")));
      process.kill(-group, signal);
      const completion = await awaitProgressObservation(closed);
      assert.ok(completion.kind === "signalled" || completion.status !== 0);
      assert.doesNotMatch(output(), /verify-progress passed/);
      await requireOwnedVerificationGroupAbsent(group);
    });
  });
}
