import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AccessRequestSchema, AccessRequestState, GetAccessRequestResponseSchema, RequestActorSchema, RequestApprovalSchema } from "@/gen/portcullis/v1/access_requests_pb";

const client = vi.hoisted(() => ({ create: vi.fn(), updateDraft: vi.fn(), get: vi.fn(), listRequestableConnections: vi.fn() }));
vi.mock("@/shared/api/client", () => ({ requestsClient: client }));

import { createAccessRequest, getAccessRequest, loadTargets, resetAccessRequests, setMayListAccessRequests, targetsStale, updateDraft } from "@/entities/request/store";
import { createDialogSession } from "@/shared/lib/dialogSession";

type DraftMutation = "create" | "updateDraft";
const mutations: DraftMutation[] = ["create", "updateDraft"];

function createRequestFixture() {
  const requester = create(RequestActorSchema, { id: "requester-1", email: "", displayName: "" });
  return create(AccessRequestSchema, {
    id: "request-1", connectionId: "connection-1", requester, version: 1n,
    state: AccessRequestState.DRAFT, effectiveState: AccessRequestState.DRAFT,
  });
}

function invokeDraftMutation(operation: DraftMutation) {
  return operation === "create"
    ? createAccessRequest("connection-1", "select 'unfinished", [])
    : updateDraft("request-1", 1n, "select 'unfinished", []);
}

beforeEach(() => {
  vi.resetAllMocks();
  resetAccessRequests();
  setMayListAccessRequests(false);
});

describe("consumed draft mutation responses", () => {
  for (const operation of mutations) {
    it(`${operation} preserves a valid unclassified draft without requiring submit-time fields`, async () => {
      const request = createRequestFixture();
      client[operation].mockResolvedValueOnce({ request });
      await expect(invokeDraftMutation(operation)).resolves.toEqual(request);
      expect(client[operation]).toHaveBeenCalledTimes(1);
    });

    it(`${operation} preserves exact versions and authored text`, async () => {
      const request = { ...createRequestFixture(), version: 9007199254740993n, title: "  월간 조회 🌱  " };
      client[operation].mockResolvedValueOnce({ request });
      await expect(invokeDraftMutation(operation)).resolves.toEqual(request);
    });

    it(`${operation} accepts an empty decision list and empty actor labels without inventing values`, async () => {
      const request = createRequestFixture();
      client[operation].mockResolvedValueOnce({ request });
      const parsed = await invokeDraftMutation(operation);
      expect(parsed).toMatchObject({ approvals: [], requester: { id: "requester-1", email: "", displayName: "" } });
    });

    it(`${operation} detaches retained fields and removes unknown response data`, async () => {
      const requester = create(RequestActorSchema, { id: "requester-1" });
      const request = { ...createRequestFixture(), requester, privateWireField: "private-wire-value" };
      client[operation].mockResolvedValueOnce({ request });
      const parsed = await invokeDraftMutation(operation);
      requester.id = "changed-after-delivery";
      expect(parsed).toMatchObject({ requester: { id: "requester-1" } });
      expect(parsed).not.toHaveProperty("privateWireField");
    });

    const invalidResponses = [
      { name: "unrelated request target", response: { request: operation === "create" ? { ...createRequestFixture(), connectionId: "other-connection" } : { ...createRequestFixture(), id: "other-request" } } },
      { name: "missing request", response: {} },
      { name: "empty request ID", response: { request: { ...createRequestFixture(), id: "" } } },
      { name: "missing requester", response: { request: create(AccessRequestSchema, { id: "private-request", connectionId: "connection-1", version: 1n, state: AccessRequestState.DRAFT, effectiveState: AccessRequestState.DRAFT }) } },
      { name: "invalid version", response: { request: { ...createRequestFixture(), version: 0n } } },
      { name: "missing approver", response: { request: { ...createRequestFixture(), approvals: [create(RequestApprovalSchema, { decision: "approved" })] } } },
    ];
    for (const { name, response } of invalidResponses) {
      it(`${operation} rejects ${name} without replaying the mutation or inventing successful data`, async () => {
        client[operation].mockResolvedValueOnce(response);
        await expect(invokeDraftMutation(operation)).rejects.toThrow("The access request response is invalid.");
        expect(client[operation]).toHaveBeenCalledTimes(1);
      });
    }

    it(`${operation} leaves the replacement session's target cache unchanged after a late malformed response`, async () => {
      let deliver = (_response: unknown): void => { throw new Error("The response was not initialized"); };
      const response = new Promise<unknown>(resolve => { deliver = resolve; });
      client[operation].mockReturnValueOnce(response);
      const { runInSession, discardSession } = createDialogSession();
      const pending = runInSession(() => invokeDraftMutation(operation));
      discardSession();
      resetAccessRequests();
      client.listRequestableConnections.mockResolvedValueOnce({ connections: [] });
      expect(await loadTargets()).toBe("ok");
      deliver({});
      await expect(pending).resolves.toEqual({ status: "superseded" });
      expect(targetsStale()).toBe(false);
    });
  }
});

describe("required decision actor responses", () => {
  const decisions = [
    { decision: "approved", valid: true, displayName: "  검토자 🌱  ", email: "reviewer@example.com" },
    { decision: "approved", valid: false, displayName: "", email: "reviewer@example.com" },
    { decision: "rejected", valid: false, displayName: "Reviewer", email: "" },
    { decision: "rejected", valid: false, displayName: "", email: "" },
  ];
  for (const decision of decisions) {
    it(`preserves ${decision.decision} evidence with valid=${decision.valid} and label ${decision.displayName}/${decision.email}`, async () => {
      const request = createRequestFixture();
      const approval = create(RequestApprovalSchema, {
        approver: { id: "reviewer-1", displayName: decision.displayName, email: decision.email },
        decision: decision.decision, valid: decision.valid, reason: "  reviewed  ",
      });
      const response = create(GetAccessRequestResponseSchema, { request: { ...request, approvals: [approval] } });
      client.get.mockResolvedValueOnce(response);
      await expect(getAccessRequest(request.id)).resolves.toEqual(response);
    });
  }

  const invalidApprovals = [
    { name: "missing actor", approval: create(RequestApprovalSchema, { decision: "approved" }) },
    { name: "empty actor ID", approval: create(RequestApprovalSchema, { approver: {}, decision: "approved" }) },
    { name: "malformed actor label", approval: { ...create(RequestApprovalSchema, { decision: "approved" }), approver: { $typeName: "portcullis.v1.RequestActor", id: "private-actor", email: 42, displayName: "private-name" } } },
    { name: "wrong actor message", approval: { ...create(RequestApprovalSchema, { decision: "approved" }), approver: { $typeName: "portcullis.v1.OtherActor", id: "private-actor", email: "private@example.com", displayName: "private-name" } } },
  ];
  for (const { name, approval } of invalidApprovals) {
    it(`rejects ${name} before displaying decision evidence`, async () => {
      const request = createRequestFixture();
      client.get.mockResolvedValueOnce({ ...create(GetAccessRequestResponseSchema, { request }), request: { ...request, approvals: [approval] } });
      await expect(getAccessRequest(request.id)).rejects.toThrow("The access request response is invalid.");
    });
  }
});
