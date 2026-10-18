import { createRoot } from "solid-js";
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it, vi } from "vitest";

import { createManagementAction } from "@/features/administration/action";

const metadata = vi.hoisted(() => ({ authorizationKey: "initial-policy" }));
vi.mock("@/entities/session/store", () => ({ load: vi.fn(async () => {}), sessionAuthorizationKey: () => metadata.authorizationKey }));

/** Controls response timing at the server-preparation boundary. */
function deferred<Value>() {
  let settle: (value: Value) => void = () => { throw new Error("not initialized"); };
  const promise = new Promise<Value>(resolve => { settle = resolve; });
  return { promise, settle };
}

describe("management intents across asynchronous route lifetimes", () => {
  it("delivers a fresh receipt once and prevents concurrent double submission", async () => {
    const scope = createRoot(dispose => ({ action: createManagementAction(), dispose }));
    const pending = deferred<string>(); let writes = 0;
    const first = scope.action.run(async () => { writes++; return pending.promise; }, "Created.");
    await scope.action.run(async () => { writes++; return "duplicate"; });
    pending.settle("fresh token");
    expect((await first)?.value).toBe("fresh token"); expect(writes).toBe(1);
    expect(scope.action.message()).toBe("Created."); scope.dispose();
  });
  it("refuses a write after a route closes during server validation", async () => {
    const scope = createRoot(dispose => ({ action: createManagementAction(), dispose }));
    const validation = deferred<void>(); let writes = 0;
    const intent = scope.action.run(async isCurrent => { await validation.promise; if (!isCurrent()) throw new Error("superseded"); writes++; return "committed"; });
    scope.dispose(); validation.settle();
    expect(await intent).toBeUndefined(); expect(writes).toBe(0); expect(scope.action.message()).toBe("");
  });
  it("never delivers an old setup token after identity or route disposal", async () => {
    const scope = createRoot(dispose => ({ action: createManagementAction(), dispose }));
    const pending = deferred<string>(); const intent = scope.action.run(() => pending.promise);
    scope.dispose(); pending.settle("previous user's setup token");
    expect(await intent).toBeUndefined(); expect(scope.action.busy()).toBe(false);
  });
  it("refuses a prepared write when policy changes while the local creation route remains open", async () => {
    metadata.authorizationKey = "before-validation";
    const scope = createRoot(dispose => ({ action: createManagementAction(), dispose }));
    const validation = deferred<void>(); let writes = 0;
    const intent = scope.action.run(async isCurrent => { await validation.promise; if (!isCurrent()) throw new ConnectError("Policy changed", Code.Aborted); writes++; return "must not write"; });
    metadata.authorizationKey = "after-validation";
    validation.settle();
    expect(await intent).toBeUndefined(); expect(writes).toBe(0);
    expect(scope.action.locked()).toBe(true); scope.dispose();
  });
  it("delivers an already issued creation receipt across a revision-only refresh without replaying its write", async () => {
    metadata.authorizationKey = "before-creation";
    const scope = createRoot(dispose => ({ action: createManagementAction(), dispose }));
    const pending = deferred<string>(); let writes = 0;
    const intent = scope.action.run(() => { writes++; return pending.promise; });
    metadata.authorizationKey = "after-creation";
    pending.settle("committed receipt");
    expect((await intent)?.value).toBe("committed receipt"); expect(writes).toBe(1); scope.dispose();
  });
  it("releases a failed precondition without reporting success or raw diagnostics", async () => {
    await createRoot(async dispose => {
      const action = createManagementAction();
      await action.run(async () => { throw new ConnectError("secret", Code.FailedPrecondition); });
      expect(action.error()).toContain("last administrator"); expect(action.error()).not.toContain("secret");
      expect(action.message()).toBe("");
      expect((await action.run(async () => "changed after fresh input"))?.value).toBe("changed after fresh input"); dispose();
    });
  });
  it.each([Code.Aborted, Code.Unavailable, Code.DeadlineExceeded, Code.Internal])("requires explicit reloading after stale or uncertain outcome %s", async code => {
    await createRoot(async dispose => {
      const action = createManagementAction(); let writes = 0;
      await action.run(async () => { writes++; throw new ConnectError("opaque diagnostic", code); });
      await action.run(async () => { writes++; return "must not replay"; });
      expect(writes).toBe(1); expect(action.locked()).toBe(true); expect(action.message()).toBe(""); dispose();
    });
  });
  it.each([Code.PermissionDenied, Code.Unauthenticated, Code.NotFound])("clears protected state and locks after admission failure %s", async code => {
    await createRoot(async dispose => {
      const clear = vi.fn(); const action = createManagementAction(clear);
      await action.run(async () => { throw new ConnectError("opaque", code); });
      expect(clear).toHaveBeenCalledOnce(); expect(action.locked()).toBe(true); dispose();
    });
  });
});
