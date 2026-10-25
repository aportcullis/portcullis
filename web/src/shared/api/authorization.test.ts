import { Code, createContextValues, type ContextValues } from "@connectrpc/connect";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { createOwnedInteractionAuthorizationContext, observeAuthorizationFailures, type AuthorizationFailure } from "@/shared/api/authorization";
import { executionsClient } from "@/shared/api/client";

type ExecutionOperation = "execute" | "cancel";
type AuthorizationRefusalScenario = {
  code: Code;
  wireCode: "unauthenticated" | "permission_denied";
  status: number;
  notification: AuthorizationFailure;
};
const operations: ExecutionOperation[] = ["execute", "cancel"];
const refusalScenarios: AuthorizationRefusalScenario[] = [
  { code: Code.Unauthenticated, wireCode: "unauthenticated", status: 401, notification: { kind: "session" } },
  { code: Code.PermissionDenied, wireCode: "permission_denied", status: 403, notification: { kind: "permission", scope: "interaction" } },
];

function invokeExecution(operation: ExecutionOperation, contextValues: ContextValues) {
  const input = { requestId: "00000000-0000-4000-8000-000000000002" };
  if (operation === "execute") return executionsClient.execute(input, { contextValues });
  return executionsClient.cancel(input, { contextValues });
}

beforeEach(() => {
  vi.stubGlobal("document", { cookie: "" });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("owned execution refusal notifications", () => {
  for (const operation of operations) {
    for (const scenario of refusalScenarios) {
      it(`keeps a current ${operation} ${scenario.status} refusal synchronous and visible to its interaction`, async () => {
        const failures: AuthorizationFailure[] = [];
        const unsubscribe = observeAuthorizationFailures(failure => { failures.push(failure); });
        vi.stubGlobal("fetch", async () => new Response(JSON.stringify({ code: scenario.wireCode, message: "request refused" }), {
          status: scenario.status, headers: { "Content-Type": "application/json" },
        }));
        try {
          const request = invokeExecution(operation, createOwnedInteractionAuthorizationContext(() => true));
          await expect(request).rejects.toMatchObject({ code: scenario.code });
          expect(failures).toEqual([scenario.notification]);
        } finally { unsubscribe(); }
      });

      it(`rejects an obsolete ${operation} ${scenario.status} without refusing the later interaction`, async () => {
        let current = true;
        const failures: AuthorizationFailure[] = [];
        const unsubscribe = observeAuthorizationFailures(failure => { failures.push(failure); });
        vi.stubGlobal("fetch", async () => {
          current = false;
          return new Response(JSON.stringify({ code: scenario.wireCode, message: "request refused" }), {
            status: scenario.status, headers: { "Content-Type": "application/json" },
          });
        });
        try {
          const request = invokeExecution(operation, createOwnedInteractionAuthorizationContext(() => current));
          await expect(request).rejects.toMatchObject({ code: scenario.code });
          expect(failures).toEqual([]);
        } finally { unsubscribe(); }
      });
    }
  }

  it("retains conservative interaction ownership for an unmarked call", async () => {
    const failures: AuthorizationFailure[] = [];
    const unsubscribe = observeAuthorizationFailures(failure => { failures.push(failure); });
    vi.stubGlobal("fetch", async () => new Response(JSON.stringify({ code: "permission_denied", message: "request refused" }), {
      status: 403, headers: { "Content-Type": "application/json" },
    }));
    try {
      await expect(invokeExecution("execute", createContextValues())).rejects.toMatchObject({ code: Code.PermissionDenied });
      expect(failures).toEqual([{ kind: "permission", scope: "interaction" }]);
    } finally { unsubscribe(); }
  });
});
