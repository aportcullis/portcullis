import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";

import { readPackageWitnessFile } from "#verification/package-witness";

/** Tracks the one process group owned by a disposable verification fixture. */
export type VerificationProcessState = { kind: "idle" } | { kind: "running"; processGroup: number };

/** Supplies only the filesystem and process ownership needed by a command. */
export type VerificationContext = {
  directory: string;
  environment: NodeJS.ProcessEnv;
  processState: VerificationProcessState;
};

/** Chooses ordinary completion or a controlled ready-Go-process operation. */
export type VerificationAction =
  | { kind: "complete" }
  | { kind: "signal"; signal: "SIGINT" | "SIGTERM" }
  | { kind: "deadline-after-ready"; milliseconds: number };

/** Preserves the native exit-code or signal absence reported by Node. */
export type VerificationResult = { status: number | null; signal: NodeJS.Signals | null; output: string };

type CommandExit = { kind: "exited"; status: number | null; signal: NodeJS.Signals | null };
type CommandFailure = { kind: "failed"; error: unknown };
type CommandState = { kind: "running" } | CommandExit | CommandFailure;
type Closed = { kind: "closed" };
type Deadline = { kind: "deadline" };
type ActiveProcess = { kind: "not-ready" } | { kind: "ready"; process: number };

/** Determines whether this invocation's exact owned group still exists. */
function ownedGroupExists(processGroup: number): boolean {
  assert.ok(Number.isSafeInteger(processGroup) && processGroup > 1 && processGroup !== process.pid, "Only an exact owned process group may be probed");
  try {
    process.kill(-processGroup, 0);
    return true;
  } catch (error: unknown) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
    return false;
  }
}

/** Requires natural group exit before fallback termination could hide a failure. */
async function requireOwnedGroupAbsent(processGroup: number): Promise<void> {
  const deadline = Date.now() + 3000;
  while (ownedGroupExists(processGroup)) {
    assert.ok(Date.now() < deadline, "The exact owned test group did not exit");
    await new Promise<void>((resolve) => setTimeout(resolve, 25));
  }
}

/** Bounds stream closure even when a descendant retains the command's pipes. */
async function requireCommandStreamsClosed(closed: Promise<Closed>): Promise<void> {
  const expiration = Promise.withResolvers<Deadline>();
  const timer = setTimeout(() => expiration.resolve({ kind: "deadline" }), 3000);
  try {
    const result = await Promise.race([closed, expiration.promise]);
    assert.equal(result.kind, "closed", "Owned command streams did not close after process termination");
  } finally {
    clearTimeout(timer);
  }
}

/** Executes one real command with a deadline and one strictly owned process group. */
export async function executeOwnedVerification(
  context: VerificationContext,
  command: string,
  arguments_: readonly string[],
  action: VerificationAction = { kind: "complete" },
): Promise<VerificationResult> {
  assert.equal(context.processState.kind, "idle", "Fixture commands must run sequentially");
  const child = spawn(command, arguments_, { cwd: context.directory, env: context.environment, detached: true, stdio: "pipe" });
  const finished = Promise.withResolvers<CommandExit | CommandFailure>();
  const closed = Promise.withResolvers<Closed>();
  const expiration = Promise.withResolvers<Deadline>();
  let output = "";
  let commandState: CommandState = { kind: "running" };
  let deadlineFired = false;
  let activeProcess: ActiveProcess = { kind: "not-ready" };
  const readCommandState = (): CommandState => commandState;
  const receiveOutput = (chunk: Buffer): void => { output += chunk.toString("utf8"); };
  child.stdout.on("data", receiveOutput);
  child.stderr.on("data", receiveOutput);
  child.once("error", (error: Error) => {
    commandState = { kind: "failed", error };
    finished.resolve(commandState);
  });
  child.once("exit", (status: number | null, signal: NodeJS.Signals | null) => {
    commandState = { kind: "exited", status, signal };
    finished.resolve(commandState);
  });
  child.once("close", () => closed.resolve({ kind: "closed" }));
  if (typeof child.pid !== "number") {
    const result = await finished.promise;
    await requireCommandStreamsClosed(closed.promise);
    if (result.kind === "failed") throw result.error;
    assert.fail("The owned command process did not start");
  }
  const processGroup = child.pid;
  assert.ok(Number.isSafeInteger(processGroup) && processGroup > 1 && processGroup !== process.pid);
  context.processState = { kind: "running", processGroup };
  const expireCommand = (): void => { deadlineFired = true; expiration.resolve({ kind: "deadline" }); };
  let timer = setTimeout(expireCommand, 120000);
  const createDeadlineError = (): Error => new Error("The owned Make target exceeded its fixture deadline");
  try {
    if (action.kind !== "complete") {
      const ready = join(context.directory, "witnesses/active.ready");
      while (!existsSync(ready)) {
        if (deadlineFired) throw createDeadlineError();
        const state = readCommandState();
        if (state.kind === "failed") throw state.error;
        assert.equal(state.kind, "running", `The active package exited before readiness:\n${output}`);
        await Promise.race([new Promise<void>((resolve) => setTimeout(resolve, 25)), finished.promise, expiration.promise]);
      }
      const observations = readPackageWitnessFile(context.directory, "active");
      const [active] = observations;
      assert.equal(observations.length, 1, "Exactly one real active binary must become ready");
      assert.ok(active !== undefined, "The ready active binary must provide its observation");
      assert.equal(active.group, processGroup, "The active binary must belong to the exact owned Make job");
      activeProcess = { kind: "ready", process: active.process };
      clearTimeout(timer);
      if (action.kind === "signal") {
        timer = setTimeout(expireCommand, 5000);
        process.kill(-processGroup, action.signal);
      } else {
        assert.ok(Number.isSafeInteger(action.milliseconds) && action.milliseconds > 0);
        timer = setTimeout(expireCommand, action.milliseconds);
      }
    }
    const result = await Promise.race([finished.promise, expiration.promise]);
    if (result.kind === "deadline" || deadlineFired) throw createDeadlineError();
    if (result.kind === "failed") throw result.error;
    await requireOwnedGroupAbsent(processGroup);
    await requireCommandStreamsClosed(closed.promise);
    if (deadlineFired) throw createDeadlineError();
    if (activeProcess.kind === "ready") {
      const { process: activeIdentifier } = activeProcess;
      assert.throws(() => process.kill(activeIdentifier, 0), { code: "ESRCH" }, "The active binary must exit without fallback termination");
    }
    return { status: result.status, signal: result.signal, output };
  } finally {
    clearTimeout(timer);
    child.stdin.destroy();
    try {
      if (ownedGroupExists(processGroup)) process.kill(-processGroup, "SIGKILL");
    } finally {
      await requireCommandStreamsClosed(closed.promise);
    }
    await requireOwnedGroupAbsent(processGroup);
    context.processState = { kind: "idle" };
  }
}
