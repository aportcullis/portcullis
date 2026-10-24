import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import { assertPackageTargetSucceeded, cancelReadyPackageTarget, readPackageWitnesses, runPackageTarget, withPackageFixture, writePackageWithoutTests, writeWitnessPackage } from "./package-scheduling-fixture.mjs";

test("sequential packages use independent real Go parents and Testcontainers sessions", async () => {
  await withPackageFixture(async (context) => {
    writeWitnessPackage(context, "first");
    writeWitnessPackage(context, "second");
    const result = await runPackageTarget(context);
    const [first, second] = assertPackageTargetSucceeded(context, result, ["first", "second"]);
    assert.notEqual(first.session, second.session, `Each package must have its own public Testcontainers session; observed Go parents ${first.parent} and ${second.parent}`);
    assert.notEqual(first.parent, second.parent, "Each package must have its own Go parent process");
  });
});

test("exact package selection retains no-test packages and excludes unrelated tests", async () => {
  await withPackageFixture(async (context) => {
    writeWitnessPackage(context, "selected");
    writeWitnessPackage(context, "unselected");
    writePackageWithoutTests(context, "library");
    const result = await runPackageTarget(context, "./selected ./library");
    assertPackageTargetSucceeded(context, result, ["selected"]);
    assert.match(result.output, /portcullis\/library\s+\[no test files\]/);
    assert.equal(readPackageWitnesses(context, "unselected").length, 0);
  });
});

test("overlapping package patterns execute each selected package exactly once", async () => {
  await withPackageFixture(async (context) => {
    writeWitnessPackage(context, "first");
    writeWitnessPackage(context, "second");
    const result = await runPackageTarget(context, "./first ./... ./second");
    const executions = assertPackageTargetSucceeded(context, result, ["first", "second"]);
    assert.equal(new Set(executions.map(({ session }) => session)).size, 2);
  });
});

test("repeated target invocations remain uncached with fresh real sessions", async () => {
  await withPackageFixture(async (context) => {
    writeWitnessPackage(context, "selected");
    assertPackageTargetSucceeded(context, await runPackageTarget(context, "./selected"), ["selected"]);
    const second = await runPackageTarget(context, "./selected");
    assert.equal(second.status, 0, second.output);
    assert.doesNotMatch(second.output, /\(cached\)/);
    const executions = readPackageWitnesses(context, "selected");
    assert.equal(executions.length, 2);
    assert.notEqual(executions[0].parent, executions[1].parent);
    assert.notEqual(executions[0].session, executions[1].session);
  });
});

test("race target expands and executes packages enabled only by the race build context", async () => {
  await withPackageFixture(async (context) => {
    writeWitnessPackage(context, "normal");
    writeWitnessPackage(context, "race_only", "pass", "race");
    assertPackageTargetSucceeded(context, await runPackageTarget(context), ["normal"]);
    assert.equal(readPackageWitnesses(context, "race_only").length, 0);
    const raced = await runPackageTarget(context, "./...", "test-race");
    assert.equal(raced.status, 0, raced.output);
    assert.equal(readPackageWitnesses(context, "normal").length, 2);
    assert.equal(readPackageWitnesses(context, "race_only").length, 1);
  });
});

test("an assertion refusal fails the target while later selected packages still execute", async () => {
  await withPackageFixture(async (context) => {
    writeWitnessPackage(context, "failed", "fail");
    writeWitnessPackage(context, "later");
    const result = await runPackageTarget(context, "./failed ./later");
    assert.notEqual(result.status, 0);
    assert.match(result.output, /intentional package assertion refusal/);
    assert.equal(readPackageWitnesses(context, "failed").length, 1);
    assert.equal(readPackageWitnesses(context, "later").length, 1);
  });
});

test("a test compilation refusal fails the target while later packages still execute", async () => {
  await withPackageFixture(async (context) => {
    writeWitnessPackage(context, "broken", "compile-error");
    writeWitnessPackage(context, "later");
    const result = await runPackageTarget(context, "./broken ./later");
    assert.notEqual(result.status, 0);
    assert.match(result.output, /undefined: missingPackageSchedulingSymbol/);
    assert.equal(readPackageWitnesses(context, "broken").length, 0);
    assert.equal(readPackageWitnesses(context, "later").length, 1);
  });
});

test("an invalid package selection refuses before starting any valid selected test", async () => {
  await withPackageFixture(async (context) => {
    writeWitnessPackage(context, "valid");
    const result = await runPackageTarget(context, "./valid ./missing");
    assert.notEqual(result.status, 0);
    assert.match(result.output, /missing/);
    assert.equal(readPackageWitnesses(context, "valid").length, 0);
  });
});

test("an empty pattern expansion refuses before any package executes", async () => {
  await withPackageFixture(async (context) => {
    writeWitnessPackage(context, "valid");
    mkdirSync(join(context.directory, "empty"));
    const result = await runPackageTarget(context, "./empty/...");
    assert.notEqual(result.status, 0);
    assert.match(result.output, /Package selection contains no Go packages\./);
    assert.equal(readPackageWitnesses(context, "valid").length, 0);
  });
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  test(`${signal} cancels the active real Go job without admitting a later package`, async () => {
    await withPackageFixture(async (context) => {
      writeWitnessPackage(context, "active", "wait");
      writeWitnessPackage(context, "later");
      await cancelReadyPackageTarget(context, signal);
    });
  });
}

test("an owned command deadline refuses and reaps the ready Go job before fixture disposal", async () => {
  await withPackageFixture(async (context) => {
    writeWitnessPackage(context, "active", "wait");
    writeWitnessPackage(context, "later");
    await assert.rejects(runPackageTarget(context, "./active ./later", "test", { kind: "deadline-after-ready", milliseconds: 25 }), {
      message: "The owned Make target exceeded its fixture deadline",
    });
    const active = readPackageWitnesses(context, "active");
    assert.equal(active.length, 1);
    assert.throws(() => process.kill(active[0].process, 0), { code: "ESRCH" });
    assert.throws(() => process.kill(-active[0].group, 0), { code: "ESRCH" });
    assert.equal(readPackageWitnesses(context, "later").length, 0);
  });
});
