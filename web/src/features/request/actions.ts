import type { PermissionKey } from "@/entities/session/store";
import type { AccessRequest } from "@/gen/portcullis/v1/access_requests_pb";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";

// Use effective state for action affordances so lazy expiry cannot leave actions enabled on expired requests.

const CANCELLABLE: readonly AccessRequestState[] = [
  AccessRequestState.DRAFT,
  AccessRequestState.PENDING,
  AccessRequestState.APPROVED,
];

// Check requests.create for owner actions and each decision’s own permission for reviewer actions (ADR-0008).
export type PermissionCheck = (permission: PermissionKey) => boolean;

// mayCancel: the requester may withdraw their own request while it is still live (§4.4 — terminal states are never revisited).
export function mayCancel(
  request: AccessRequest,
  isOwner: boolean,
  hasPermission: PermissionCheck,
): boolean {
  return isOwner && hasPermission("requests.create") && CANCELLABLE.includes(request.effectiveState);
}

// mayApprove / mayReject: only a pending request, and never its requester (§4.3 forbids self-approval, admin included). The caller additionally gates each button on its own permission key (requests.approve / requests.reject).
export function mayApprove(request: AccessRequest, isOwner: boolean): boolean {
  return !isOwner && request.effectiveState === AccessRequestState.PENDING;
}

export function mayReject(request: AccessRequest, isOwner: boolean): boolean {
  return mayApprove(request, isOwner);
}

// mayEditDraft: a draft is the only editable state, and only its owner edits it.
export function mayEditDraft(
  request: AccessRequest,
  isOwner: boolean,
  hasPermission: PermissionCheck,
): boolean {
  return (
    isOwner &&
    hasPermission("requests.create") &&
    request.effectiveState === AccessRequestState.DRAFT
  );
}

// maySubmitDraft: the owner continues a saved draft — from the list row, or from the detail view. The create dialog closes on "Save draft", and a submit refused by policy also leaves a draft behind (§4.3), so without this the draft is a dead end — saved, editable, and impossible to send (ADR-0018:100 promises the opposite).
export function maySubmitDraft(
  request: AccessRequest,
  isOwner: boolean,
  hasPermission: PermissionCheck,
): boolean {
  return mayEditDraft(request, isOwner, hasPermission);
}

// Offer Save draft only with requests.list so the owner can reopen it; submission requires no later list access.
export function mayReturnToSavedDraft(hasPermission: PermissionCheck): boolean {
  return hasPermission("requests.list");
}
