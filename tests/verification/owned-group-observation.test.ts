import assert from "node:assert/strict";
import test from "node:test";

import { releaseOwnedLeader, requireOwnedGroupPresent, requireOwnedGroupTerminated, stageOwnedDarwinZombie, withOwnedGroup, type OwnedGroupFixture } from "#verification/owned-group-fixture";
import { requireOwnedVerificationGroupAbsent, terminateOwnedVerificationGroup } from "#verification/verification-process";

type WaitObservation = { kind: "absent" } | { kind: "failed"; error: unknown };
type ReapingFailure = { kind: "none" } | { kind: "failed"; error: unknown };

/** Attaches rejection capture immediately, before the test yields to native reaping. */
function captureGroupWait(processGroup: number): Promise<WaitObservation> {
  return requireOwnedVerificationGroupAbsent(processGroup).then(
    (): WaitObservation => ({ kind: "absent" }),
    (error: unknown): WaitObservation => ({ kind: "failed", error }),
  );
}

/** Preserves a waiter refusal alongside any failed native lifecycle proof. */
async function reapAndObserve(context: OwnedGroupFixture, pending: Promise<WaitObservation>): Promise<WaitObservation> {
  let failure: ReapingFailure = { kind: "none" };
  try {
    await requireOwnedGroupTerminated(context);
  } catch (error: unknown) {
    failure = { kind: "failed", error };
  }
  const observation = await pending;
  if (failure.kind === "failed") {
    if (observation.kind === "failed") throw new AggregateError([observation.error, failure.error], "Owned group waiting and native reaping both failed");
    throw failure.error;
  }
  return observation;
}

/** Requires successful absence observation after native cleanup has been proved. */
function requireSuccessfulWait(observation: WaitObservation): void {
  if (observation.kind === "failed") throw observation.error;
}

/** Checks a captured native error only after the owned child has been reaped. */
function requireNativeErrorCode(error: unknown, code: string): void {
  assert.ok(error instanceof Error && "code" in error);
  assert.equal(error.code, code);
}

test("group observation accepts an already absent owned group", async () => {
  await withOwnedGroup("leader-only", async (context) => {
    await releaseOwnedLeader(context);
    await requireOwnedGroupTerminated(context);
    await requireOwnedVerificationGroupAbsent(context.processGroup);
  });
});

test("group observation waits for a live owned leader's controlled release", async () => {
  await withOwnedGroup("leader-only", async (context) => {
    requireOwnedGroupPresent(context);
    const pending = captureGroupWait(context.processGroup);
    await releaseOwnedLeader(context);
    requireSuccessfulWait(await reapAndObserve(context, pending));
  });
});

test("group observation accepts a killed and natively reaped owned leader", async () => {
  await withOwnedGroup("leader-only", async (context) => {
    terminateOwnedVerificationGroup(context.processGroup);
    await requireOwnedGroupTerminated(context);
    await requireOwnedVerificationGroupAbsent(context.processGroup);
  });
});

test("group observation waits for an inherited descendant after its leader exits", async () => {
  await withOwnedGroup("leader-descendant", async (context) => {
    await releaseOwnedLeader(context);
    requireOwnedGroupPresent(context);
    const pending = captureGroupWait(context.processGroup);
    terminateOwnedVerificationGroup(context.processGroup);
    requireSuccessfulWait(await reapAndObserve(context, pending));
  });
});

test("group observation refuses a still-live owned group at its unchanged deadline", async () => {
  await withOwnedGroup("leader-only", async (context) => {
    requireOwnedGroupPresent(context);
    await assert.rejects(requireOwnedVerificationGroupAbsent(context.processGroup), {
      code: "ERR_ASSERTION", message: "The exact owned test group did not exit",
    });
    requireOwnedGroupPresent(context);
  });
});

for (const processGroup of [0, 1]) {
  test(`group observation refuses invalid group ${processGroup} before an OS probe`, async () => {
    await assert.rejects(requireOwnedVerificationGroupAbsent(processGroup), {
      code: "ERR_ASSERTION", message: "Only an exact owned process group may be probed",
    });
  });
}

test("group observation propagates native PID validation refusal before an OS probe", async () => {
  await assert.rejects(requireOwnedVerificationGroupAbsent(Number.MAX_SAFE_INTEGER), { code: "ERR_INVALID_ARG_TYPE" });
});

if (process.platform === "darwin") {
  test("group observation waits through a real unreaped Darwin zombie", async () => {
    await withOwnedGroup("leader-only", async (context) => {
      const staged = stageOwnedDarwinZombie(context);
      let settlement: { kind: "pending" } | { kind: "settled" } = { kind: "pending" };
      const readSettlement = (): { kind: "pending" } | { kind: "settled" } => settlement;
      const pending = requireOwnedVerificationGroupAbsent(context.processGroup).then(
        (): WaitObservation => { settlement = { kind: "settled" }; return { kind: "absent" }; },
        (error: unknown): WaitObservation => { settlement = { kind: "settled" }; return { kind: "failed", error }; },
      );
      await Promise.resolve();
      const beforeReaping = readSettlement();
      let probe: { kind: "returned" } | { kind: "failed"; error: unknown } = { kind: "returned" };
      try {
        process.kill(-context.processGroup, 0);
      } catch (error: unknown) {
        probe = { kind: "failed", error };
      }
      let observation: WaitObservation;
      try {
        observation = await reapAndObserve(context, pending);
      } catch (error: unknown) {
        if (staged.kind === "failed") throw new AggregateError([staged.error, error], "Owned zombie staging and lifecycle observation both failed");
        throw error;
      }
      if (staged.kind === "failed") throw staged.error;
      requireNativeErrorCode(staged.probeError, "EPERM");
      assert.equal(staged.termination.kind, "failed", "Actual termination must still propagate EPERM");
      if (staged.termination.kind !== "failed") assert.fail("The unreaped group unexpectedly accepted termination");
      requireNativeErrorCode(staged.termination.error, "EPERM");
      assert.equal(probe.kind, "failed", "The microtask checkpoint must precede native reaping");
      if (probe.kind !== "failed") assert.fail("The owned zombie group unexpectedly accepted the checkpoint probe");
      requireNativeErrorCode(probe.error, "EPERM");
      requireSuccessfulWait(observation);
      assert.equal(beforeReaping.kind, "pending", "Owned-group absence must remain pending before native reaping");
    });
  });
}
