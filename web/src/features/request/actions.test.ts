import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";

import type { AccessRequest } from "@/gen/portcullis/v1/access_requests_pb";
import { AccessRequestSchema, AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import type { PermissionKey } from "@/entities/session/store";
import {
  mayApprove,
  mayCancel,
  mayEditDraft,
  mayReject,
  mayReturnToSavedDraft,
  maySubmitDraft,
  validateRejectReason,
} from "@/features/request/actions";

// Use effective state for action affordances so lazy expiry cannot leave actions enabled on expired requests.
type RequestOptions = {
  state: AccessRequestState;
  effectiveState: AccessRequestState;
};

const request = (options: RequestOptions): AccessRequest =>
  create(AccessRequestSchema, {
    id: "req-1",
    version: 1n,
    state: options.state,
    effectiveState: options.effectiveState,
  });


const holder = (...keys: PermissionKey[]) => (key: PermissionKey) => keys.includes(key);
const owner = true;

const requester = holder("requests.list", "requests.get", "requests.create");

describe("request owner actions require requests.create", () => {
  const draft = () =>
    request({ state: AccessRequestState.DRAFT, effectiveState: AccessRequestState.DRAFT });
  const pending = () =>
    request({ state: AccessRequestState.PENDING, effectiveState: AccessRequestState.PENDING });

  it("offers the owner nothing to change without requests.create", () => {

    const can = holder("requests.list", "requests.get");
    expect(maySubmitDraft(draft(), owner, can)).toBe(false);
    expect(mayEditDraft(draft(), owner, can)).toBe(false);
    expect(mayCancel(pending(), owner, can)).toBe(false);
  });

  it("offers them once requests.create is held", () => {
    const can = holder("requests.list", "requests.get", "requests.create");
    expect(maySubmitDraft(draft(), owner, can)).toBe(true);
    expect(mayEditDraft(draft(), owner, can)).toBe(true);
    expect(mayCancel(pending(), owner, can)).toBe(true);
  });

  it("still withholds them from a non-owner who holds requests.create", () => {
    const can = holder("requests.create");
    expect(maySubmitDraft(draft(), !owner, can)).toBe(false);
    expect(mayEditDraft(draft(), !owner, can)).toBe(false);
    expect(mayCancel(pending(), !owner, can)).toBe(false);
  });

  it("keeps the state rules on top of the permission", () => {

    const can = holder("requests.create");
    const cancelled = request({
      state: AccessRequestState.CANCELLED,
      effectiveState: AccessRequestState.CANCELLED,
    });
    expect(mayCancel(cancelled, owner, can)).toBe(false);
    expect(maySubmitDraft(cancelled, owner, can)).toBe(false);
  });

  it("leaves the reviewer decisions on their own keys", () => {
    // Regression: approve/reject must not start depending on requests.create.
    const reviewer = holder("requests.approve", "requests.reject");
    expect(mayApprove(pending(), !owner)).toBe(true);
    expect(mayReject(pending(), !owner)).toBe(true);
    expect(maySubmitDraft(draft(), owner, reviewer)).toBe(false);
  });
});

describe("request actions", () => {

  it("lets the owner submit their own draft, and nobody else", () => {
    const draft = request({
      state: AccessRequestState.DRAFT,
      effectiveState: AccessRequestState.DRAFT,
    });
    expect(maySubmitDraft(draft, true, requester)).toBe(true);

    expect(maySubmitDraft(draft, false, requester)).toBe(false);
  });

  it("offers no submit outside the draft state", () => {
    for (const state of [
      AccessRequestState.PENDING,
      AccessRequestState.APPROVED,
      AccessRequestState.REJECTED,
      AccessRequestState.EXPIRED,
      AccessRequestState.CANCELLED,
    ]) {
      const other = request({ state, effectiveState: state });
      expect(maySubmitDraft(other, true, requester)).toBe(false);
    }
  });

  it("offers no submit on a draft whose connection archive already cancelled it", () => {
    // Stored draft, effectively cancelled by the archive cascade: judging by the stored state would show a Submit the server refuses.
    const swept = request({
      state: AccessRequestState.DRAFT,
      effectiveState: AccessRequestState.CANCELLED,
    });
    expect(maySubmitDraft(swept, true, requester)).toBe(false);
  });

  it("offers no action on a request whose approval has expired", () => {

    const expired = request({
      state: AccessRequestState.APPROVED,
      effectiveState: AccessRequestState.EXPIRED,
    });
    expect(mayCancel(expired, true, requester)).toBe(false);
    expect(mayApprove(expired, false)).toBe(false);
    expect(mayReject(expired, false)).toBe(false);
  });

  it("lets the owner cancel a live draft, pending, or approved request", () => {
    for (const state of [
      AccessRequestState.DRAFT,
      AccessRequestState.PENDING,
      AccessRequestState.APPROVED,
    ]) {
      const live = request({ state, effectiveState: state });
      expect(mayCancel(live, true, requester)).toBe(true);

      expect(mayCancel(live, false, requester)).toBe(false);
    }
  });

  it("lets a non-owner decide only on a pending request", () => {
    const pending = request({
      state: AccessRequestState.PENDING,
      effectiveState: AccessRequestState.PENDING,
    });
    expect(mayApprove(pending, false)).toBe(true);
    expect(mayReject(pending, false)).toBe(true);

    expect(mayApprove(pending, true)).toBe(false);
    expect(mayReject(pending, true)).toBe(false);

    const approved = request({
      state: AccessRequestState.APPROVED,
      effectiveState: AccessRequestState.APPROVED,
    });
    expect(mayApprove(approved, false)).toBe(false);
  });
});


describe("saving a draft requires a way back to it", () => {
  it("offers Save only when the caller can list requests", () => {
    expect(mayReturnToSavedDraft(holder("requests.create", "requests.list"))).toBe(true);
    expect(mayReturnToSavedDraft(requester)).toBe(true);
  });

  it("withholds Save from a create-only caller, whatever else they hold", () => {
    expect(mayReturnToSavedDraft(holder("requests.create"))).toBe(false);

    expect(mayReturnToSavedDraft(holder("requests.create", "requests.get"))).toBe(false);

    expect(
      mayReturnToSavedDraft(holder("requests.create", "requests.approve", "requests.reject")),
    ).toBe(false);
    expect(mayReturnToSavedDraft(holder())).toBe(false);
  });
});


describe("list-row actions", () => {
  const draftOf = (state: AccessRequestState) => request({ state, effectiveState: state });

  it("offers the owner Submit and Cancel on a draft without requests.get", () => {
    const listOnly = holder("requests.create", "requests.list");
    const draft = draftOf(AccessRequestState.DRAFT);
    expect(maySubmitDraft(draft, owner, listOnly)).toBe(true);
    expect(mayCancel(draft, owner, listOnly)).toBe(true);
  });

  it("offers nothing on another user's request, under any keys", () => {
    const everything = holder(
      "requests.create",
      "requests.list",
      "requests.get",
      "requests.approve",
      "requests.reject",
    );
    for (const state of [
      AccessRequestState.DRAFT,
      AccessRequestState.PENDING,
      AccessRequestState.APPROVED,
    ]) {
      const foreign = draftOf(state);
      expect(maySubmitDraft(foreign, !owner, everything)).toBe(false);
      expect(mayCancel(foreign, !owner, everything)).toBe(false);
    }
  });

  it("offers Submit only on a draft, and Cancel only while live", () => {
    const everything = holder("requests.create", "requests.list", "requests.get");
    expect(maySubmitDraft(draftOf(AccessRequestState.PENDING), owner, everything)).toBe(false);
    expect(maySubmitDraft(draftOf(AccessRequestState.APPROVED), owner, everything)).toBe(false);
    expect(mayCancel(draftOf(AccessRequestState.CANCELLED), owner, everything)).toBe(false);
    expect(mayCancel(draftOf(AccessRequestState.EXPIRED), owner, everything)).toBe(false);
  });
});

// The server refuses a rejection whose reason is empty after trimming whitespace (access.ErrReasonRequired), so the form says so before sending.
describe("reject reason", () => {
  it.each(["Out of scope for this change window", "  needs a WHERE clause  ", "🙅", "x"])("accepts the reason %j", (reason) => {
    expect(validateRejectReason(reason)).toBe("");
  });

  it.each(["", "   ", "\n\t", "　"])("refuses the blank reason %j", (reason) => {
    expect(validateRejectReason(reason)).toBe("Enter a reason to reject this request.");
  });
});
