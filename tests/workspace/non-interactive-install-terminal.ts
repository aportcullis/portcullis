import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { existsSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { purgePrompt, readInstallOwnershipInputs, readLefthookExecutable, runInstallCommand, type InstallFixture } from "#workspace/non-interactive-install-fixture";
import { requireJsonRecord } from "#workspace/workspace-fixture";

type HookResult = { status: number | null; output: string };
type TerminalOutcome = "completed" | "timeout" | "excess-output" | "prompt" | "prompt-after-completion";
type ForceStop = { kind: "inactive" } | { kind: "active"; timer: ReturnType<typeof setTimeout> };
type CallbackFailure = { kind: "none" } | { kind: "failed"; error: unknown };

/** Signals only a process group owned and recorded by this fixture. */
function signalOwnedProcessGroup(processGroup: number, signal: NodeJS.Signals): void {
  assert.ok(Number.isSafeInteger(processGroup) && processGroup > 1 && processGroup !== process.pid);
  try {
    process.kill(-processGroup, signal);
  } catch (error: unknown) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
  }
}

/** Reports whether an exact owned process group still exists. */
function ownedProcessGroupExists(processGroup: number): boolean {
  try {
    process.kill(-processGroup, 0);
    return true;
  } catch (error: unknown) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
    return false;
  }
}

/** Confirms only recorded groups have stopped before removing their fixture. */
async function requireOwnedProcessGroupsStopped(processGroups: ReadonlySet<number>): Promise<void> {
  const deadline = Date.now() + 3000;
  while (true) {
    const remaining = [...processGroups].filter(ownedProcessGroupExists);
    if (remaining.length === 0) return;
    for (const processGroup of remaining) signalOwnedProcessGroup(processGroup, "SIGKILL");
    assert.ok(Date.now() < deadline, `Owned hook process groups did not stop: ${remaining.join(", ")}`);
    await new Promise<void>((resolve) => setTimeout(resolve, 25));
  }
}

/** Runs real Lefthook with a terminal while keeping hook reference input on a pipe. */
export async function installThroughHookTerminal(context: InstallFixture, changes: NodeJS.ProcessEnv = {}): Promise<HookResult> {
  assert.ok(process.platform === "darwin" || process.platform === "linux", "The hook fixture requires a Unix pseudo-terminal");
  const python = runInstallCommand("python3", ["-I", "-S", "-c", "import pty"], context.directory, context.environment);
  assert.equal(python.status, 0, `Python3 with its standard-library PTY module is required:\n${python.output}`);
  const inputs = readInstallOwnershipInputs(context);
  rmSync(join(context.directory, "verification-witness.json"), { force: true });
  const wrapper = join(context.directory, "hook-wrapper.ts");
  writeFileSync(join(context.directory, "hook-config.json"), JSON.stringify({
    executable: readLefthookExecutable(),
    referenceInput: `refs/heads/main ${"a".repeat(40)} refs/heads/main ${"b".repeat(40)}\n`,
    processGroupFile: "wrapper-process-group",
    statusFile: "hook-status.json",
  }));
  const terminalProgram = "import pty, sys\npty.spawn(sys.argv[1:])\n";
  const child = spawn("python3", ["-I", "-S", "-c", terminalProgram, process.execPath, wrapper], { cwd: context.directory, env: { ...context.environment, ...changes }, detached: true, stdio: "pipe" });
  assert.ok(typeof child.pid === "number");
  const outerProcessGroup = child.pid;
  let output = "";
  let outcome: TerminalOutcome = "completed";
  let stopping = false;
  let forceStop: ForceStop = { kind: "inactive" };
  let callbackFailure: CallbackFailure = { kind: "none" };
  const readOutcome = (): TerminalOutcome => outcome;
  const readForceStop = (): ForceStop => forceStop;
  const readCallbackFailure = (): CallbackFailure => callbackFailure;
  const processGroups = new Set([outerProcessGroup]);
  const recordOwnedProcessGroups = (): number => {
    let innermostProcessGroup = outerProcessGroup;
    for (const record of ["wrapper-process-group", "hook-process-group"]) {
      const processGroupPath = join(context.directory, record);
      if (existsSync(processGroupPath)) {
        const processGroup = Number(readFileSync(processGroupPath, "utf8").trim());
        assert.ok(Number.isSafeInteger(processGroup) && processGroup > 1 && processGroup !== process.pid);
        processGroups.add(processGroup);
        innermostProcessGroup = processGroup;
      }
    }
    return innermostProcessGroup;
  };
  const captureCallbackFailure = (error: unknown): void => {
    callbackFailure = { kind: "failed", error };
    child.kill("SIGKILL");
  };
  const stop = (): void => {
    if (stopping) return;
    stopping = true;
    forceStop = { kind: "active", timer: setTimeout(() => {
      try {
        recordOwnedProcessGroups();
        for (const processGroup of processGroups) {
          if (ownedProcessGroupExists(processGroup)) signalOwnedProcessGroup(processGroup, "SIGKILL");
        }
      } catch (error: unknown) {
        captureCallbackFailure(error);
      }
    }, 2000) };
    const innermostProcessGroup = recordOwnedProcessGroups();
    if (ownedProcessGroupExists(innermostProcessGroup)) signalOwnedProcessGroup(innermostProcessGroup, "SIGTERM");
  };
  const timeout = setTimeout(() => {
    outcome = "timeout";
    try { stop(); } catch (error: unknown) { captureCallbackFailure(error); }
  }, 30000);
  const receive = (chunk: Buffer): void => {
    try {
      output += chunk.toString("utf8");
      if (output.length > 2 * 1024 * 1024) { outcome = "excess-output"; stop(); }
      if (outcome === "completed" && purgePrompt.test(output)) {
        outcome = child.exitCode === null && !existsSync(join(context.directory, "hook-status.json")) ? "prompt" : "prompt-after-completion";
        stop();
      }
    } catch (error: unknown) {
      captureCallbackFailure(error);
    }
  };
  child.stdout.on("data", receive);
  child.stderr.on("data", receive);
  child.stdin.on("error", (error: Error) => { if (!("code" in error) || error.code !== "EPIPE") captureCallbackFailure(error); });
  try {
    await new Promise<void>((resolve, reject) => { child.once("error", reject); child.once("close", () => resolve()); });
  } finally {
    clearTimeout(timeout);
    child.stdin.destroy();
    const forceStopState = readForceStop();
    if (forceStopState.kind === "active") clearTimeout(forceStopState.timer);
    recordOwnedProcessGroups();
    await requireOwnedProcessGroupsStopped(processGroups);
  }
  const failure = readCallbackFailure();
  if (failure.kind === "failed") throw failure.error;
  assert.deepEqual(readInstallOwnershipInputs(context), inputs, "The real hook must preserve frozen ownership inputs");
  const result = readOutcome();
  if (result !== "completed") return { status: 1, output: `${result}: ${output}` };
  assert.equal(existsSync(join(context.directory, "hook-status.json")), true, `The terminal wrapper must report the actual hook status:\n${output}`);
  const parsed: unknown = JSON.parse(readFileSync(join(context.directory, "hook-status.json"), "utf8"));
  requireJsonRecord(parsed);
  const { status, signal } = parsed;
  assert.ok(status === null || (typeof status === "number" && Number.isSafeInteger(status)));
  assert.equal(signal, null, output);
  return { status, output };
}
