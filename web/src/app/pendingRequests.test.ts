import { create, toBinary } from "@bufbuild/protobuf";
import { Code } from "@connectrpc/connect";
import { createRoot } from "solid-js";
import { afterEach, assert, beforeEach, describe, expect, it, vi } from "vitest";

import { createPendingRequestCount } from "@/app/pendingRequests";
import { load } from "@/entities/session/store";
import { ListAccessRequestsResponseSchema } from "@/gen/portcullis/v1/access_requests_pb";
import { MeResponseSchema } from "@/gen/portcullis/v1/auth_pb";
import { observeAuthorizationFailures, type AuthorizationFailure } from "@/shared/api/authorization";
import { requestsClient, rolesClient } from "@/shared/api/client";
import { accessRequestTotalCountSchema } from "@/shared/api/accessRequestValidation";

type RefusalCode = "permission_denied" | "unauthenticated" | "unavailable";
const staleRefusals: RefusalCode[] = ["permission_denied", "unauthenticated"];

function holdResponse() {
  let deliver = (_response: Response): void => { throw new Error("Response was not initialized"); };
  const response = new Promise<Response>(resolve => { deliver = resolve; });
  return { response, deliver };
}

function createAuthenticatedResponse(authorizationRevision: bigint): Response {
  return new Response(toBinary(MeResponseSchema, create(MeResponseSchema, {
    user: { id: "current-operator", status: "active" },
    permissions: ["requests.list", "requests.approve", "roles.create"],
    capabilities: ["requests.list", "requests.approve", "roles.create"],
    authorizationRevision,
  })), { status: 200, headers: { "Content-Type": "application/proto" } });
}

function createPendingCountResponse(totalCount: bigint): Response {
  return new Response(toBinary(ListAccessRequestsResponseSchema, create(ListAccessRequestsResponseSchema, { totalCount })), {
    status: 200, headers: { "Content-Type": "application/proto" },
  });
}

function createRefusedResponse(code: RefusalCode): Response {
  const status = code === "permission_denied" ? 403 : code === "unauthenticated" ? 401 : 503;
  return new Response(JSON.stringify({ code, message: "request refused" }), {
    status, headers: { "Content-Type": "application/json" },
  });
}

async function openPendingCounter() {
  let authorizationRevision = 7n;
  const reads: ReturnType<typeof holdResponse>[] = [];
  const listCalls = vi.spyOn(requestsClient, "list");
  const fetchResponse = vi.fn((url: unknown): Promise<Response> => {
    if (typeof url !== "string") throw new Error("Expected a Connect URL");
    if (url.endsWith("/portcullis.v1.Auth/Me")) return Promise.resolve(createAuthenticatedResponse(authorizationRevision));
    if (url.endsWith("/portcullis.v1.AccessRequests/List")) {
      const held = holdResponse();
      reads.push(held);
      return held.response;
    }
    if (url.endsWith("/portcullis.v1.Roles/Create")) return Promise.resolve(createRefusedResponse("permission_denied"));
    throw new Error("Unexpected RPC");
  });
  vi.stubGlobal("fetch", fetchResponse);
  await load();
  const failures: AuthorizationFailure[] = [];
  const unsubscribe = observeAuthorizationFailures(failure => { failures.push(failure); });
  const scope = createRoot(dispose => ({ count: createPendingRequestCount(), dispose }));
  function readPendingCountResponse(index: number) {
    const read = reads[index];
    assert.isDefined(read, "The pending read has not arrived");
    return read;
  }
  async function deliverPendingCountResponse(index: number, response: Response) {
    const call = listCalls.mock.results[index];
    assert.isDefined(call, "The pending call has not started");
    if (call.type !== "return") throw new Error("The pending call has not returned its promise");
    readPendingCountResponse(index).deliver(response);
    return Promise.allSettled([call.value]);
  }
  return {
    ...scope, failures, reads, readPendingCountResponse, deliverPendingCountResponse,
    advanceRevision() { authorizationRevision++; },
    close() { scope.dispose(); unsubscribe(); listCalls.mockRestore(); },
  };
}

beforeEach(() => {
  vi.stubGlobal("window", new EventTarget());
  vi.stubGlobal("document", Object.assign(new EventTarget(), { hidden: false, cookie: "" }));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("automatic pending-count authorization failures", () => {
  it("identifies a held background read refusal separately from the current creation interaction", async () => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      scope.readPendingCountResponse(0).deliver(createRefusedResponse("permission_denied"));
      await vi.waitFor(() => expect(scope.failures).toHaveLength(1));
      expect(scope.failures).toEqual([{ kind: "permission", scope: "background-read" }]);
      expect(scope.count()).toEqual({ kind: "unavailable" });
    } finally {
      scope.close();
    }
  });

  it("updates the protected badge after a successful background read", async () => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      scope.readPendingCountResponse(0).deliver(createPendingCountResponse(4n));
      await vi.waitFor(() => expect(scope.count()).toEqual({ kind: "ready", totalCount: 4n }));
      expect(scope.failures).toEqual([]);
    } finally { scope.close(); }
  });

  it.each([0n, 9223372036854775807n])("retains the valid pending total %s as known data", async totalCount => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      expect(await scope.deliverPendingCountResponse(0, createPendingCountResponse(totalCount))).toMatchObject([{ status: "fulfilled" }]);
      expect(scope.count()).toEqual({ kind: "ready", totalCount });
    } finally { scope.close(); }
  });

  it.each(["4", 4, false, {}])("rejects malformed consumed count data %j at the existing field contract", input => {
    expect(accessRequestTotalCountSchema.safeParse(input).success).toBe(false);
  });

  it("rejects a negative pending total instead of retaining an invalid count or inventing zero", async () => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      expect(await scope.deliverPendingCountResponse(0, createPendingCountResponse(-1n))).toMatchObject([{ status: "fulfilled" }]);
      expect(scope.count()).toEqual({ kind: "unavailable" });
      expect(scope.failures).toEqual([]);
    } finally { scope.close(); }
  });

  it("removes a previously valid pending total when its next response is invalid", async () => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      expect(await scope.deliverPendingCountResponse(0, createPendingCountResponse(4n))).toMatchObject([{ status: "fulfilled" }]);
      window.dispatchEvent(new Event("online"));
      await vi.waitFor(() => expect(scope.reads).toHaveLength(2));
      expect(await scope.deliverPendingCountResponse(1, createPendingCountResponse(-1n))).toMatchObject([{ status: "fulfilled" }]);
      expect(scope.count()).toEqual({ kind: "unavailable" });
      expect(scope.failures).toEqual([]);
    } finally { scope.close(); }
  });

  it("does not duplicate a held read when reconnect events overlap", async () => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      window.dispatchEvent(new Event("online"));
      window.dispatchEvent(new Event("online"));
      scope.readPendingCountResponse(0).deliver(createPendingCountResponse(4n));
      await vi.waitFor(() => expect(scope.count()).toEqual({ kind: "ready", totalCount: 4n }));
      expect(scope.reads).toHaveLength(1);
      window.dispatchEvent(new Event("online"));
      await vi.waitFor(() => expect(scope.reads).toHaveLength(2));
      scope.readPendingCountResponse(1).deliver(createPendingCountResponse(9n));
      await vi.waitFor(() => expect(scope.count()).toEqual({ kind: "ready", totalCount: 9n }));
    } finally { scope.close(); }
  });

  it("removes a previously visible badge after background permission is refused", async () => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      scope.readPendingCountResponse(0).deliver(createPendingCountResponse(4n));
      await vi.waitFor(() => expect(scope.count()).toEqual({ kind: "ready", totalCount: 4n }));
      window.dispatchEvent(new Event("online"));
      await vi.waitFor(() => expect(scope.reads).toHaveLength(2));
      scope.readPendingCountResponse(1).deliver(createRefusedResponse("permission_denied"));
      await vi.waitFor(() => expect(scope.count()).toEqual({ kind: "unavailable" }));
      expect(scope.failures).toEqual([{ kind: "permission", scope: "background-read" }]);
    } finally { scope.close(); }
  });

  it("clears the refused badge while the global coordinator resolves fresh same-authority metadata", async () => {
    const scope = await openPendingCounter();
    const refreshes: Promise<void>[] = [];
    const unsubscribe = observeAuthorizationFailures(() => { refreshes.push(load()); });
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      expect(await scope.deliverPendingCountResponse(0, createPendingCountResponse(4n))).toMatchObject([{ status: "fulfilled" }]);
      expect(scope.count()).toEqual({ kind: "ready", totalCount: 4n });
      window.dispatchEvent(new Event("online"));
      await vi.waitFor(() => expect(scope.reads).toHaveLength(2));
      expect(await scope.deliverPendingCountResponse(1, createRefusedResponse("permission_denied"))).toMatchObject([{ status: "rejected" }]);
      expect(await Promise.allSettled(refreshes)).toMatchObject([{ status: "fulfilled" }]);
      expect(scope.count()).toEqual({ kind: "unavailable" });
    } finally { unsubscribe(); scope.close(); }
  });

  it("retains the last count on an ordinary transport outage without declaring permission loss", async () => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      scope.readPendingCountResponse(0).deliver(createPendingCountResponse(4n));
      await vi.waitFor(() => expect(scope.count()).toEqual({ kind: "ready", totalCount: 4n }));
      window.dispatchEvent(new Event("online"));
      await vi.waitFor(() => expect(scope.reads).toHaveLength(2));
      const outcomes = await scope.deliverPendingCountResponse(1, createRefusedResponse("unavailable"));
      expect(outcomes).toMatchObject([{ status: "rejected" }]);
      expect(scope.count()).toEqual({ kind: "ready", totalCount: 4n });
      expect(scope.failures).toEqual([]);
    } finally { scope.close(); }
  });

  it("reports session loss from a current background read without downgrading it to permission refresh", async () => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      scope.readPendingCountResponse(0).deliver(createRefusedResponse("unauthenticated"));
      await vi.waitFor(() => expect(scope.failures).toHaveLength(1));
      expect(scope.failures).toEqual([{ kind: "session" }]);
      expect(scope.count()).toEqual({ kind: "unavailable" });
    } finally { scope.close(); }
  });

  it("keeps direct Role creation refusal in the current interaction scope", async () => {
    const scope = await openPendingCounter();
    try {
      await expect(rolesClient.create({ name: "New reader", permissions: ["requests.get"] })).rejects.toMatchObject({ code: Code.PermissionDenied });
      expect(scope.failures).toEqual([{ kind: "permission", scope: "interaction" }]);
    } finally { scope.close(); }
  });

  it("does not immediately replay a persistently refused read after same-snapshot Me but permits explicit and scheduled refresh", async () => {
    vi.useFakeTimers();
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      expect(await scope.deliverPendingCountResponse(0, createRefusedResponse("permission_denied"))).toMatchObject([{ status: "rejected" }]);
      await load();
      expect(scope.reads).toHaveLength(1);
      window.dispatchEvent(new Event("online"));
      await vi.waitFor(() => expect(scope.reads).toHaveLength(2));
      expect(await scope.deliverPendingCountResponse(1, createRefusedResponse("permission_denied"))).toMatchObject([{ status: "rejected" }]);
      await load();
      expect(scope.reads).toHaveLength(2);
      await vi.advanceTimersByTimeAsync(30_000);
      expect(scope.reads).toHaveLength(3);
      expect(await scope.deliverPendingCountResponse(2, createPendingCountResponse(6n))).toMatchObject([{ status: "fulfilled" }]);
      expect(scope.count()).toEqual({ kind: "ready", totalCount: 6n });
    } finally { scope.close(); }
  });

  it("refreshes a changed authority snapshot and fences the older read's failure", async () => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      scope.advanceRevision();
      await load();
      await vi.waitFor(() => expect(scope.reads).toHaveLength(2));
      expect(await scope.deliverPendingCountResponse(1, createPendingCountResponse(6n))).toMatchObject([{ status: "fulfilled" }]);
      expect(await scope.deliverPendingCountResponse(0, createRefusedResponse("unauthenticated"))).toMatchObject([{ status: "rejected" }]);
      expect(scope.failures).toEqual([]);
      expect(scope.count()).toEqual({ kind: "ready", totalCount: 6n });
    } finally { scope.close(); }
  });

  it.each(staleRefusals)("fences a held %s after same-snapshot Me and lets the next explicit read use the fresh session", async code => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      await load();
      expect(scope.reads).toHaveLength(1);
      expect(await scope.deliverPendingCountResponse(0, createRefusedResponse(code))).toMatchObject([{ status: "rejected" }]);
      expect(scope.failures).toEqual([]);
      window.dispatchEvent(new Event("online"));
      await vi.waitFor(() => expect(scope.reads).toHaveLength(2));
      expect(await scope.deliverPendingCountResponse(1, createPendingCountResponse(6n))).toMatchObject([{ status: "fulfilled" }]);
      expect(scope.count()).toEqual({ kind: "ready", totalCount: 6n });
    } finally { scope.close(); }
  });

  it.each(staleRefusals)("does not let a disposed background %s response refuse a later interaction", async code => {
    const scope = await openPendingCounter();
    try {
      await vi.waitFor(() => expect(scope.reads).toHaveLength(1));
      scope.dispose();
      const outcomes = await scope.deliverPendingCountResponse(0, createRefusedResponse(code));
      expect(outcomes).toMatchObject([{ status: "rejected" }]);
      expect(scope.failures).toEqual([]);
    } finally {
      scope.close();
    }
  });
});
