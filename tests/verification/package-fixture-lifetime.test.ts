import assert, { AssertionError } from "node:assert/strict";
import { existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { requireOwnedGroupPresent, requireOwnedGroupTerminated, withOwnedGroup, type OwnedGroupFixture } from "#verification/owned-group-fixture";
import { withPackageFixture } from "#verification/package-scheduling-fixture";
import type { VerificationContext } from "#verification/verification-process";

type PrimaryFailure = { kind: "none" } | { kind: "throw"; value: unknown };
type PackageRecord = { kind: "not-recorded" } | { kind: "recorded"; context: VerificationContext; group: OwnedGroupFixture };
type FailureRecord = { kind: "not-recorded" } | { kind: "recorded"; error: unknown; directory: string };
type OuterFailure = { kind: "none" } | { kind: "failed"; error: unknown };

/** Recognizes native aggregate contents without trusting their untyped payloads. */
function isFailureList(value: unknown): value is readonly unknown[] {
  return Array.isArray(value);
}

/** Verifies the actual ownership guard remains a distinct error object. */
function requireOwnershipFailure(error: unknown, directory: string): void {
  assert.ok(error instanceof AssertionError, "The fixture must retain its ownership assertion");
  assert.ok(error.message.includes(`Owned processes remain active; fixture retained at ${directory}`));
  assert.equal(error.actual, "running");
  assert.equal(error.expected, "idle");
}

/** Captures a real active-group refusal, disposing its directory only after strict reaping. */
async function captureActivePackageFailure(primary: PrimaryFailure): Promise<{ error: unknown; directory: string }> {
  const records: { package: PackageRecord; failure: FailureRecord } = { package: { kind: "not-recorded" }, failure: { kind: "not-recorded" } };
  let outerFailure: OuterFailure = { kind: "none" };
  try {
    await withOwnedGroup("leader-only", async (group) => {
      try {
        await withPackageFixture(async (context) => {
          records.package = { kind: "recorded", context, group };
          context.processState = { kind: "running", processGroup: group.processGroup };
          requireOwnedGroupPresent(group);
          if (primary.kind === "throw") throw primary.value;
        });
      } catch (error: unknown) {
        assert.equal(records.package.kind, "recorded", "The controlled callback must run before refusal");
        if (records.package.kind !== "recorded") throw error;
        records.failure = { kind: "recorded", error, directory: records.package.context.directory };
      }
      assert.equal(records.package.kind, "recorded");
      assert.equal(records.failure.kind, "recorded", "An active owned group must refuse fixture disposal");
      if (records.package.kind !== "recorded") assert.fail("The owned package context was not recorded");
      const { context } = records.package;
      assert.deepEqual(context.processState, { kind: "running", processGroup: group.processGroup });
      requireOwnedGroupPresent(group);
      assert.equal(existsSync(context.directory), true, "The directory must remain while its real owned group is alive");
    });
  } catch (error: unknown) {
    outerFailure = { kind: "failed", error };
  }
  try {
    if (records.package.kind === "recorded") {
      const { context, group } = records.package;
      await requireOwnedGroupTerminated(group);
      assert.ok(context.directory.startsWith(join(tmpdir(), "portcullis-package-scheduling-")));
      rmSync(context.directory, { recursive: true, force: true });
      assert.equal(existsSync(context.directory), false, "The exact retained fixture must be removed after reaping");
    }
  } catch (error: unknown) {
    if (outerFailure.kind === "failed") throw new AggregateError([outerFailure.error, error], "Active package scenario and retained-fixture disposal both failed");
    throw error;
  }
  if (outerFailure.kind === "failed") throw outerFailure.error;
  if (records.failure.kind !== "recorded") assert.fail("The active package fixture did not record its refusal");
  return { error: records.failure.error, directory: records.failure.directory };
}

/** Requires both original and ownership failures without replacing their objects. */
function requireCombinedFailure(error: unknown, primary: unknown, directory: string): void {
  assert.ok(error instanceof AggregateError, "Both the primary failure and live ownership refusal must remain visible");
  const errors: unknown = error.errors;
  assert.ok(isFailureList(errors));
  assert.equal(errors.length, 2);
  const [preserved, ownership] = errors;
  assert.equal(preserved, primary, "The original thrown value must retain its identity");
  requireOwnershipFailure(ownership, directory);
}

test("an idle package fixture preserves its callback error and removes its directory", async () => {
  const primary = new Error("controlled idle callback failure");
  const contexts: VerificationContext[] = [];
  try {
    await assert.rejects(withPackageFixture(async (context) => {
      contexts.push(context);
      throw primary;
    }), (error: unknown) => error === primary);
    assert.equal(contexts.length, 1);
    const [context] = contexts;
    assert.ok(context !== undefined);
    assert.equal(context.processState.kind, "idle");
    assert.equal(existsSync(context.directory), false);
  } finally {
    for (const context of contexts) {
      assert.equal(context.processState.kind, "idle");
      rmSync(context.directory, { recursive: true, force: true });
      assert.equal(existsSync(context.directory), false);
    }
  }
});

test("an active package fixture retains its directory and reports ownership refusal", async () => {
  const { error, directory } = await captureActivePackageFailure({ kind: "none" });
  requireOwnershipFailure(error, directory);
});

test("an active package fixture preserves its callback Error alongside ownership refusal", async () => {
  const primary = new Error("controlled active callback failure");
  const { error, directory } = await captureActivePackageFailure({ kind: "throw", value: primary });
  requireCombinedFailure(error, primary, directory);
});

test("an active package fixture preserves a non-Error value alongside ownership refusal", async () => {
  const primary: unknown = Object.freeze({ kind: "controlled non-Error callback failure" });
  const { error, directory } = await captureActivePackageFailure({ kind: "throw", value: primary });
  requireCombinedFailure(error, primary, directory);
});
