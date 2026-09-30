import { create } from "@bufbuild/protobuf";
import { describe, expect, it, vi } from "vitest";

import type {
  AccessRequest,
  ListAccessRequestsResponse,
  RequestableConnection,
} from "@/gen/portcullis/v1/access_requests_pb";
import {
  AccessRequestSchema,
  ListAccessRequestsResponseSchema,
  RequestableConnectionSchema,
} from "@/gen/portcullis/v1/access_requests_pb";

// The request store's race guards and its list-consistency rule live here. Both need interleavings and filter state Playwright cannot schedule deterministically, so they are pinned with deferred promises and a mocked client (the connection store's precedent).
const client = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  updateDraft: vi.fn(),
  submit: vi.fn(),
  cancel: vi.fn(),
  approve: vi.fn(),
  reject: vi.fn(),
  get: vi.fn(),
  listRequestableConnections: vi.fn(),
}));
vi.mock("@/shared/api/client", () => ({ requestsClient: client }));

import {
  accessRequests,
  approveAccessRequest,
  createAccessRequest,
  invalidateTargets,
  listError,
  loadAccessRequests,
  loadTargets,
  resetAccessRequests,
  setFilter,
  setMayListAccessRequests,
  targetError,
  targets,
  targetsStale,
  targetState,
  totalCount,
  updateDraft,
} from "@/entities/request/store";


type CreateResult = { request: AccessRequest };
type TargetsResult = { connections: RequestableConnection[] };
type Deferred<T> = { promise: Promise<T>; resolve: (value: T) => void };

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}


const req = (id: string, state = 0): AccessRequest =>
  create(AccessRequestSchema, { id, version: 1n, state, effectiveState: state });

const target = (id: string): RequestableConnection =>
  create(RequestableConnectionSchema, { id, displayName: id });

const page = (items: AccessRequest[], total: bigint): ListAccessRequestsResponse =>
  create(ListAccessRequestsResponseSchema, {
    items,
    page: 1,
    pageSize: 20,
    totalCount: total,
    totalPages: total === 0n ? 0 : 1,
  });

describe("request store principal fence", () => {
  it("drops a mutation response that resolves after a principal switch", async () => {
    resetAccessRequests();
    const d = deferred<CreateResult>();
    client.create.mockReturnValueOnce(d.promise);


    const pending = createAccessRequest("conn-1", "select secret", []);
    resetAccessRequests();


    d.resolve({ request: req("req-a") });
    await pending;

    // The late response must NOT enter the new principal's cache.
    expect(accessRequests()).toHaveLength(0);
    expect(totalCount()).toBe(0n);
  });
});


describe("request store mutations vs the list refresh", () => {
  it("returns the created request when the caller cannot list", async () => {
    resetAccessRequests();
    client.list.mockClear();

    setMayListAccessRequests(false);
    client.create.mockResolvedValueOnce({ request: req("req-created") });

    await expect(createAccessRequest("conn-1", "select 1", [])).resolves.toMatchObject({
      id: "req-created",
    });
    expect(client.list).not.toHaveBeenCalled();
    setMayListAccessRequests(true);
  });

  it("keeps the mutation's result when the refresh itself fails", async () => {
    resetAccessRequests();
    client.create.mockResolvedValueOnce({ request: req("req-created") });
    client.list.mockRejectedValueOnce(new Error("boom"));

    // The create succeeded; a failed re-read is news about the LIST, not about the create, and it must not surface as a rejected mutation.
    await expect(createAccessRequest("conn-1", "select 1", [])).resolves.toMatchObject({
      id: "req-created",
    });
    expect(listError()).not.toBe("");
  });

  it("treats a refused draft edit as a reason to distrust the target list", async () => {
    resetAccessRequests();
    client.listRequestableConnections.mockResolvedValueOnce({ connections: [target("conn-1")] });
    await loadTargets();
    expect(targetsStale()).toBe(false);


    client.updateDraft.mockRejectedValueOnce(new Error("archived"));
    await expect(updateDraft("req-1", 1n, "select 1", [])).rejects.toThrow();

    expect(targetsStale()).toBe(true);
  });

  it("still refreshes when the caller can list", async () => {
    resetAccessRequests();
    client.create.mockResolvedValueOnce({ request: req("req-created") });
    client.list.mockResolvedValue(page([req("req-created")], 1n));

    await createAccessRequest("conn-1", "select 1", []);
    expect(client.list).toHaveBeenCalled();
    expect(accessRequests().map((r) => r.id)).toEqual(["req-created"]);
  });
});

describe("request store target list", () => {
  it("reports a failed load as state instead of throwing it away", async () => {
    resetAccessRequests();
    client.listRequestableConnections.mockRejectedValueOnce(new Error("boom"));

    // The caller fires this without awaiting (opening a dialog), so a rejection would become an unhandled rejection and the picker would sit empty with no explanation. The failure must land in state the UI can render — and the OUTCOME must come back, because "we could not ask" and "the server says there are none" call for different UI.
    expect(await loadTargets()).toBe("error");

    expect(targetState()).toBe("error");
    expect(targetError()).not.toBe("");
    expect(targets()).toHaveLength(0);
  });

  it("keeps the last known list when a refresh fails", async () => {
    resetAccessRequests();
    client.listRequestableConnections.mockResolvedValueOnce({
      connections: [target("conn-1"), target("conn-2")],
    });
    await loadTargets();

    // A failed REFRESH says nothing about which connections exist — only that we could not ask. Emptying the list here is how a network blip becomes "your connection was archived", and the caller then drops a selection the user cannot re-pick (a saved draft locks its connection field).
    client.listRequestableConnections.mockRejectedValueOnce(new Error("boom"));
    expect(await loadTargets()).toBe("error");

    expect(targets().map((c) => c.id)).toEqual(["conn-1", "conn-2"]);
    expect(targetState()).toBe("error");
  });

  it("distinguishes a superseded load from a failure", async () => {
    resetAccessRequests();
    const first = deferred<TargetsResult>();
    client.listRequestableConnections.mockReturnValueOnce(first.promise);
    const stale = loadTargets();
    invalidateTargets();
    first.resolve({ connections: [target("conn-old")] });


    expect(await stale).toBe("superseded");
    expect(targetError()).toBe("");
  });

  it("reports an empty server answer as success, not as a failure", async () => {
    resetAccessRequests();
    // Every target archived, or the caller lost access: a legitimate answer the UI must treat differently from "we could not ask".
    client.listRequestableConnections.mockResolvedValueOnce({ connections: [] });

    expect(await loadTargets()).toBe("ok");
    expect(targetState()).toBe("ready");
    expect(targets()).toHaveLength(0);
  });

  it("refetches after invalidation so an archived target cannot linger", async () => {
    resetAccessRequests();
    client.listRequestableConnections.mockClear();
    client.listRequestableConnections.mockResolvedValueOnce({ connections: [target("conn-1")] });
    await loadTargets();
    expect(targets().map((c) => c.id)).toEqual(["conn-1"]);

    // A request refused because the target was archived invalidates the cache; the next load must go back to the server rather than reuse the stale row.
    invalidateTargets();
    client.listRequestableConnections.mockResolvedValueOnce({ connections: [target("conn-2")] });
    await loadTargets();

    expect(targets().map((c) => c.id)).toEqual(["conn-2"]);
    expect(client.listRequestableConnections).toHaveBeenCalledTimes(2);
  });

  it("drops a load invalidated mid-flight, so a refused target cannot come back", async () => {
    resetAccessRequests();
    const inflight = deferred<TargetsResult>();
    client.listRequestableConnections.mockReturnValueOnce(inflight.promise);

    // The dialog opens and starts a load; while it is in flight the create is refused because the target was archived, which invalidates the cache. The load that was already reading the OLD world must not land afterwards.
    const pending = loadTargets();
    invalidateTargets();
    inflight.resolve({ connections: [target("conn-archived")] });
    await pending;

    expect(targets()).toHaveLength(0);
    expect(targetState()).toBe("idle");
  });

  it("clears a previous error once a later load succeeds", async () => {
    resetAccessRequests();
    client.listRequestableConnections.mockRejectedValueOnce(new Error("boom"));
    await loadTargets();
    expect(targetState()).toBe("error");

    client.listRequestableConnections.mockResolvedValueOnce({ connections: [target("conn-1")] });
    await loadTargets();

    expect(targetState()).toBe("ready");
    expect(targetError()).toBe("");
    expect(targets().map((c) => c.id)).toEqual(["conn-1"]);
  });

  it("keeps the open picker intact when a create is refused, marking it stale", async () => {
    resetAccessRequests();
    client.listRequestableConnections.mockResolvedValueOnce({
      connections: [target("conn-1"), target("conn-2")],
    });
    await loadTargets();
    expect(targetState()).toBe("ready");

    // The server refuses. The refusal must reach the caller and mark the cache stale — but the dialog is OPEN, so blanking the options would take the user's choices away mid-edit with nothing to explain it (and only closing the dialog, which also wipes the SQL, could bring them back).
    client.create.mockRejectedValueOnce(new Error("archived"));
    await expect(createAccessRequest("conn-1", "select 1", [])).rejects.toThrow();

    expect(targets().map((c) => c.id)).toEqual(["conn-1", "conn-2"]);
    expect(targetState()).toBe("ready");
    expect(targetsStale()).toBe(true);
  });

  it("refetches on the next load while stale, and clears the flag", async () => {
    resetAccessRequests();
    client.listRequestableConnections.mockResolvedValueOnce({ connections: [target("conn-1")] });
    await loadTargets();
    client.create.mockRejectedValueOnce(new Error("archived"));
    await expect(createAccessRequest("conn-1", "select 1", [])).rejects.toThrow();
    expect(targetsStale()).toBe(true);

    client.listRequestableConnections.mockResolvedValueOnce({ connections: [target("conn-2")] });
    await loadTargets();

    expect(targets().map((c) => c.id)).toEqual(["conn-2"]);
    expect(targetsStale()).toBe(false);
  });

  it("still clears everything on a principal switch", async () => {
    resetAccessRequests();
    client.listRequestableConnections.mockResolvedValueOnce({ connections: [target("conn-1")] });
    await loadTargets();
    expect(targets()).toHaveLength(1);

    // A new principal may target different connections, so the previous user's list must not survive the switch — clearing stays correct HERE.
    resetAccessRequests();

    expect(targets()).toHaveLength(0);
    expect(targetState()).toBe("idle");
  });

  it("drops a slower earlier load so it cannot overwrite the newest list", async () => {
    resetAccessRequests();
    const first = deferred<TargetsResult>();
    const second = deferred<TargetsResult>();
    client.listRequestableConnections.mockReturnValueOnce(first.promise);
    client.listRequestableConnections.mockReturnValueOnce(second.promise);

    const a = loadTargets();
    const b = loadTargets();
    second.resolve({ connections: [target("conn-new")] });
    await b;
    first.resolve({ connections: [target("conn-old")] });
    await a;

    expect(targets().map((c) => c.id)).toEqual(["conn-new"]);
  });
});

describe("request store list consistency", () => {
  it("refetches the current page after a mutation instead of merging blindly", async () => {
    resetAccessRequests();

    const pendingRow = req("req-pending", 2);
    client.list.mockResolvedValue(page([pendingRow], 1n));
    setFilter("pending");
    await loadAccessRequests();
    expect(accessRequests().map((r) => r.id)).toEqual(["req-pending"]);

    // Approving it makes it no longer match the filter. The mutation response alone cannot know that, so the store must re-read the page: the server now returns an empty pending page.
    client.approve.mockResolvedValueOnce({ request: req("req-pending", 3) });
    client.list.mockResolvedValue(page([], 0n));

    await approveAccessRequest("req-pending", "ok");

    // The approved row must be gone from the pending view, and the totals must match the server — not a locally patched guess.
    expect(accessRequests()).toHaveLength(0);
    expect(totalCount()).toBe(0n);
  });
});
