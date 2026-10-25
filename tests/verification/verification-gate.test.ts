import assert, { AssertionError } from "node:assert/strict";
import test from "node:test";

import { assertDeclaredToolsOwned, assertGateLaunched, assertGateRefused, linkDependencyToProjectTool, linkModulesToProjectTools, runCanonicalGate, runNativeGateSource, withVerificationGateFixture, writeGateSource, writeGateValueModule } from "#verification/verification-gate-fixture";

const validBody = "const count: number = 7; assert.equal(count, 7);";

test("the canonical gate accepts a valid native TypeScript test", async () => {
  await withVerificationGateFixture(async (context) => {
    writeGateSource(context, validBody);
    assertGateLaunched(context, await runCanonicalGate(context));
  });
});

test("the canonical gate accepts checked unknown values and tagged native absence", async () => {
  await withVerificationGateFixture(async (context) => {
    writeGateSource(context, `type State = { kind: "ready"; value: number } | { kind: "empty" };
  /** Recognizes a usable observation at its unknown boundary. */
  function isReady(value: unknown): value is { kind: "ready"; value: number } {
    return typeof value === "object" && value !== null && "kind" in value && value.kind === "ready"
      && "value" in value && typeof value.value === "number";
  }
  const candidate: unknown = { kind: "ready", value: 7 };
  if (!isReady(candidate)) throw new Error("Expected a checked observation");
  const state: State = candidate;
  assert.equal(state.value, 7);
  const signal: string | null = null;
  if (signal === null) assert.equal(state.kind, "ready");`);
    assertGateLaunched(context, await runCanonicalGate(context));
  });
});

test("the canonical gate resolves repository aliases and explicit type-only imports", async () => {
  await withVerificationGateFixture(async (context) => {
    writeGateValueModule(context);
    writeGateSource(context, "const observed: GateValue = gateValue; assert.equal(observed.value, 7);", 'import { gateValue, type GateValue } from "#verification/gate-value";');
    assertGateLaunched(context, await runCanonicalGate(context));
  });
});

test("a fresh frozen root install owns its compiler, Node types and linter", async () => {
  await withVerificationGateFixture(async (context) => {
    await assertDeclaredToolsOwned(context);
    writeGateSource(context, validBody);
    assertGateLaunched(context, await runCanonicalGate(context));
  });
});

test("fresh root tools remain owned through a workspace directory alias", async () => {
  await withVerificationGateFixture(async (context) => {
    writeGateSource(context, validBody);
    assertGateLaunched(context, await runCanonicalGate(context));
    await assertDeclaredToolsOwned(context);
  }, "aliased");
});

/** Recognizes the ownership refusal rather than an unrelated install or tool failure. */
function isToolOwnershipRefusal(error: unknown): boolean {
  return error instanceof AssertionError && error.message === "Tool resolution must remain in the fresh fixture modules";
}

test("tool ownership refuses one dependency linked outside the fresh fixture", async () => {
  await withVerificationGateFixture(async (context) => {
    await assertDeclaredToolsOwned(context);
    linkDependencyToProjectTool(context);
    await assert.rejects(assertDeclaredToolsOwned(context), isToolOwnershipRefusal);
  });
});

test("tool ownership refuses a whole modules root linked outside the fresh fixture", async () => {
  await withVerificationGateFixture(async (context) => {
    await assertDeclaredToolsOwned(context);
    linkModulesToProjectTools(context);
    await assert.rejects(assertDeclaredToolsOwned(context), isToolOwnershipRefusal);
  });
});

type RefusedSource = { name: string; body: string; category: RegExp };
const refusedSources: readonly RefusedSource[] = [
  { name: "a runnable incompatible assignment", body: 'const count: number = "wrong-type"; void count;', category: /TS2322/ },
  { name: "a runnable explicit-any escape", body: "const count: any = 7; assert.equal(count, 7);", category: /no-explicit-any/ },
  { name: "a runnable type assertion escape", body: 'const count = "wrong-type" as unknown as number; void count;', category: /consistent-type-assertions/ },
  { name: "a runnable checker suppression", body: '// @ts-ignore\n  const count: number = "wrong-type"; void count;', category: /ban-ts-comment/ },
];

for (const { name, body, category } of refusedSources) {
  test(`the canonical gate refuses ${name} before launch`, async () => {
    await withVerificationGateFixture(async (context) => {
      writeGateSource(context, validBody);
      assertGateLaunched(context, await runCanonicalGate(context));
      writeGateSource(context, body);
      assertGateLaunched(context, await runNativeGateSource(context));
      assertGateRefused(context, await runCanonicalGate(context), category);
    });
  });
}
