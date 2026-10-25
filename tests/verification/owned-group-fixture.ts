import assert from "node:assert/strict";
import { fork, type ChildProcess } from "node:child_process";
import { fileURLToPath } from "node:url";

import { terminateOwnedVerificationGroup } from "#verification/verification-process";

type GroupMode = "leader-only" | "leader-descendant";
type Descendant = { kind: "absent" } | { kind: "present"; process: number; parent: number };
type GroupReady = { kind: "group-ready"; leader: number; descendant: Descendant };
type NativeExit = { kind: "exited"; status: number | null; signal: NodeJS.Signals | null };
type NativeFailure = { kind: "failed"; error: Error };
type ScenarioFailure = { kind: "none" } | { kind: "failed"; error: unknown };
type TerminationObservation = { kind: "returned" } | { kind: "failed"; error: unknown };
type ZombieObservation = { kind: "observed"; probeError: Error; termination: TerminationObservation } | { kind: "failed"; error: unknown };

/** Retains only the exact native child and membership recorded by this fixture. */
export type OwnedGroupFixture = {
  processGroup: number;
  leader: ChildProcess;
  members: { kind: "not-ready" } | { kind: "ready"; value: GroupReady };
  exited: Promise<NativeExit | NativeFailure>;
  closed: Promise<void>;
  output: () => string;
};

/** Recognizes a process identifier safe for an exact owned-process operation. */
function isProcessIdentifier(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 1 && value !== process.pid;
}

/** Validates the fixture's native IPC identity without trusting parsed payloads. */
function isGroupReady(value: unknown): value is GroupReady {
  if (typeof value !== "object" || value === null || !("kind" in value) || value.kind !== "group-ready"
    || !("leader" in value) || !isProcessIdentifier(value.leader) || !("descendant" in value)) return false;
  const { descendant } = value;
  if (typeof descendant !== "object" || descendant === null || !("kind" in descendant)) return false;
  return descendant.kind === "absent" || (descendant.kind === "present"
    && "process" in descendant && isProcessIdentifier(descendant.process) && descendant.process !== value.leader
    && "parent" in descendant && descendant.parent === value.leader);
}

/** Bounds native lifecycle waits without using time to order the scenario. */
async function waitForOwnedLifecycle<Value>(pending: Promise<Value>, description: string): Promise<Value> {
  const deadline = Promise.withResolvers<never>();
  const timer = setTimeout(() => deadline.reject(new Error(description)), 5000);
  try {
    return await Promise.race([pending, deadline.promise]);
  } finally {
    clearTimeout(timer);
  }
}

/** Determines only whether this fixture's recorded process group still exists. */
function ownedGroupExists(context: OwnedGroupFixture): boolean {
  assert.ok(isProcessIdentifier(context.processGroup));
  try {
    process.kill(-context.processGroup, 0);
    return true;
  } catch (error: unknown) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
    return false;
  }
}

/** Waits for reaping of one recorded PID or its negative owned group identifier. */
async function requireOwnedIdentifierAbsent(identifier: number): Promise<void> {
  assert.ok(isProcessIdentifier(Math.abs(identifier)));
  const deadline = performance.now() + 5000;
  while (true) {
    try {
      process.kill(identifier, 0);
    } catch (error: unknown) {
      if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
      return;
    }
    assert.ok(performance.now() < deadline, "The recorded owned process or group was not reaped");
    await new Promise<void>((resolve) => setTimeout(resolve, 25));
  }
}

/** Requires all known members and their exact group to be absent after streams close. */
export async function requireOwnedGroupTerminated(context: OwnedGroupFixture): Promise<void> {
  const result = await waitForOwnedLifecycle(context.exited, `Owned leader did not exit: ${context.output()}`);
  if (result.kind === "failed") throw result.error;
  await waitForOwnedLifecycle(context.closed, `Owned group streams did not close: ${context.output()}`);
  await requireOwnedIdentifierAbsent(context.processGroup);
  assert.throws(() => process.kill(context.processGroup, 0), { code: "ESRCH" }, "The owned leader must be absent");
  if (context.members.kind === "ready" && context.members.value.descendant.kind === "present") {
    const descendantIdentifier = context.members.value.descendant.process;
    await requireOwnedIdentifierAbsent(descendantIdentifier);
    assert.throws(() => process.kill(descendantIdentifier, 0), { code: "ESRCH" }, "The owned descendant must be absent");
  }
  await requireOwnedIdentifierAbsent(-context.processGroup);
  assert.equal(ownedGroupExists(context), false, "The exact owned process group must be absent");
}

/** Proves the recorded group exists while its known members remain alive. */
export function requireOwnedGroupPresent(context: OwnedGroupFixture): void {
  assert.equal(context.members.kind, "ready");
  assert.equal(ownedGroupExists(context), true);
  if (context.members.kind === "ready" && context.members.value.descendant.kind === "present") {
    assert.equal(process.kill(context.members.value.descendant.process, 0), true);
  }
}

/** Records Darwin's unreaped single-leader group without yielding to native reaping. */
export function stageOwnedDarwinZombie(context: OwnedGroupFixture): ZombieObservation {
  assert.equal(process.platform, "darwin");
  assert.ok(context.members.kind === "ready" && context.members.value.descendant.kind === "absent");
  requireOwnedGroupPresent(context);
  try {
    process.kill(-context.processGroup, "SIGKILL");
  } catch (error: unknown) {
    return { kind: "failed", error };
  }
  const deadline = performance.now() + 250;
  while (true) {
    try {
      process.kill(-context.processGroup, 0);
    } catch (error: unknown) {
      if (!(error instanceof Error) || !("code" in error) || error.code !== "EPERM") return { kind: "failed", error };
      let termination: TerminationObservation = { kind: "returned" };
      try {
        terminateOwnedVerificationGroup(context.processGroup);
      } catch (failure: unknown) {
        termination = { kind: "failed", error: failure };
      }
      return { kind: "observed", probeError: error, termination };
    }
    if (performance.now() >= deadline) return { kind: "failed", error: new Error("The owned Darwin zombie group was not observed within the staging bound") };
  }
}

/** Releases the real leader and waits for its own exit, independently of descendants. */
export async function releaseOwnedLeader(context: OwnedGroupFixture): Promise<void> {
  const sent = Promise.withResolvers<{ kind: "sent" } | NativeFailure>();
  context.leader.send({ kind: "release-leader" }, (error: Error | null) => {
    sent.resolve(error === null ? { kind: "sent" } : { kind: "failed", error });
  });
  const delivery = await waitForOwnedLifecycle(sent.promise, "The owned leader did not receive its release");
  if (delivery.kind === "failed") throw delivery.error;
  const result = await waitForOwnedLifecycle(context.exited, "The released owned leader did not exit");
  if (result.kind === "failed") throw result.error;
  assert.equal(result.status, 0, context.output());
  assert.equal(result.signal, null);
  assert.throws(() => process.kill(context.processGroup, 0), { code: "ESRCH" });
}

/** Reaps only this group's remaining members, preserving unexpected signal failures. */
async function disposeOwnedGroup(context: OwnedGroupFixture): Promise<void> {
  if (ownedGroupExists(context)) {
    try {
      process.kill(-context.processGroup, "SIGKILL");
    } catch (error: unknown) {
      if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
    }
  }
  await requireOwnedGroupTerminated(context);
}

/** Runs one real Node process-group scenario and preserves its original refusal. */
export async function withOwnedGroup(mode: GroupMode, scenario: (context: OwnedGroupFixture) => Promise<void>): Promise<void> {
  const environment = { ...process.env };
  delete environment.NODE_TEST_CONTEXT;
  const leader = fork(fileURLToPath(new URL("./owned-group-child.ts", import.meta.url)), ["leader", mode], {
    env: environment, execArgv: [], detached: true, stdio: ["ignore", "pipe", "pipe", "ipc"],
  });
  const ready = Promise.withResolvers<GroupReady | NativeFailure>();
  const exited = Promise.withResolvers<NativeExit | NativeFailure>();
  const closed = Promise.withResolvers<void>();
  let output = "";
  const receiveOutput = (chunk: Buffer): void => { output += chunk.toString("utf8"); };
  assert.ok(leader.stdout !== null && leader.stderr !== null, "The owned child must retain captured output");
  leader.stdout.on("data", receiveOutput);
  leader.stderr.on("data", receiveOutput);
  leader.on("error", (error: Error) => { ready.resolve({ kind: "failed", error }); exited.resolve({ kind: "failed", error }); });
  leader.on("message", (value: unknown) => {
    ready.resolve(isGroupReady(value) ? value : { kind: "failed", error: new Error("The owned group returned an invalid IPC identity") });
  });
  leader.once("exit", (status: number | null, signal: NodeJS.Signals | null) => exited.resolve({ kind: "exited", status, signal }));
  leader.once("close", () => closed.resolve());
  const processGroup = leader.pid;
  if (!isProcessIdentifier(processGroup)) {
    await waitForOwnedLifecycle(closed.promise, "The unstarted child streams did not close");
    const result = await exited.promise;
    if (result.kind === "failed") throw result.error;
    assert.fail("The owned child did not supply a safe process identifier");
  }
  const context: OwnedGroupFixture = { processGroup, leader, members: { kind: "not-ready" }, exited: exited.promise, closed: closed.promise, output: () => output };
  let failure: ScenarioFailure = { kind: "none" };
  try {
    const result = await waitForOwnedLifecycle(ready.promise, `The owned group did not become ready: ${output}`);
    if (result.kind === "failed") throw result.error;
    assert.equal(result.leader, processGroup);
    assert.equal(result.descendant.kind, mode === "leader-only" ? "absent" : "present");
    context.members = { kind: "ready", value: result };
    await scenario(context);
  } catch (error: unknown) {
    failure = { kind: "failed", error };
  }
  try {
    await disposeOwnedGroup(context);
  } catch (error: unknown) {
    if (failure.kind === "failed") throw new AggregateError([failure.error, error], "Owned scenario and cleanup both failed");
    throw error;
  }
  if (failure.kind === "failed") throw failure.error;
}
