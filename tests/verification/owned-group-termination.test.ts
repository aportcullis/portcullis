import assert from "node:assert/strict";
import test from "node:test";

import { releaseOwnedLeader, requireOwnedGroupPresent, requireOwnedGroupTerminated, withOwnedGroup } from "#verification/owned-group-fixture";
import { terminateOwnedVerificationGroup } from "#verification/verification-process";

test("termination reaps a live owned group leader", async () => {
  await withOwnedGroup("leader-only", async (context) => {
    requireOwnedGroupPresent(context);
    terminateOwnedVerificationGroup(context.processGroup);
    await requireOwnedGroupTerminated(context);
  });
});

test("termination reaps a live owned leader and its inherited-group descendant", async () => {
  await withOwnedGroup("leader-descendant", async (context) => {
    requireOwnedGroupPresent(context);
    terminateOwnedVerificationGroup(context.processGroup);
    await requireOwnedGroupTerminated(context);
  });
});

test("termination reaps the owned descendant after its leader has exited", async () => {
  await withOwnedGroup("leader-descendant", async (context) => {
    requireOwnedGroupPresent(context);
    await releaseOwnedLeader(context);
    requireOwnedGroupPresent(context);
    terminateOwnedVerificationGroup(context.processGroup);
    await requireOwnedGroupTerminated(context);
  });
});

test("termination accepts an owned group that exits after the real presence probe", async () => {
  await withOwnedGroup("leader-only", async (context) => {
    requireOwnedGroupPresent(context);
    await releaseOwnedLeader(context);
    await requireOwnedGroupTerminated(context);
    assert.throws(() => process.kill(-context.processGroup, 0), { code: "ESRCH" });
    terminateOwnedVerificationGroup(context.processGroup);
    await requireOwnedGroupTerminated(context);
  });
});

test("termination propagates native PID validation refusal before any OS signal", () => {
  // Node rejects this outside-signed-32-bit PID before reaching its OS signal bridge.
  assert.throws(() => terminateOwnedVerificationGroup(Number.MAX_SAFE_INTEGER), { code: "ERR_INVALID_ARG_TYPE" });
});
