import type { AccessRequest } from "@/gen/portcullis/v1/access_requests_pb";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { stateLabel } from "@/entities/request/model";

export type WorkflowStatus = "complete" | "current" | "upcoming" | "automatic" | "unknown" | "not-run" | "attention";
export interface WorkflowNode { label: string; status: WorkflowStatus; detail: string }
export interface RequestWorkflow { nodes: WorkflowNode[]; notice: string }

/** Derives visible stage progress from server summary facts without inventing audit history. */
export function requestWorkflow(request: Pick<AccessRequest, "effectiveState" | "requiredApprovals" | "validApprovals">): RequestWorkflow {
  const nodes: WorkflowNode[] = ["Draft", "Review", "Ready", "Execution"].map(label => ({ label, status: "upcoming", detail: "Not reached" }));
  const set = (index: number, status: WorkflowStatus, detail: string) => { nodes[index] = { ...nodes[index], status, detail }; };
  const state = request.effectiveState;
  const notice = "Stage progress reflects the current state, not the full audit timeline.";
  if (state === AccessRequestState.UNSPECIFIED || !Object.values(AccessRequestState).includes(state)) {
    return { nodes: nodes.map(node => ({ ...node, status: "unknown", detail: "Unknown" })), notice: "Current state is unavailable." };
  }
  set(0, state === AccessRequestState.DRAFT ? "current" : "complete", state === AccessRequestState.DRAFT ? "Not submitted" : "Request created");
  if (state === AccessRequestState.DRAFT) return { nodes, notice };
  if ([AccessRequestState.CANCELLED, AccessRequestState.EXPIRED].includes(state)) {
    set(1, "unknown", "History unavailable"); set(2, "unknown", "History unavailable"); set(3, "not-run", "Not executed");
    return { nodes, notice: `${stateLabel(state)}. Last active stage is not available in the list. Open details for evidence.` };
  }
  if (state === AccessRequestState.PENDING || state === AccessRequestState.REJECTED) {
    set(1, state === AccessRequestState.PENDING ? "current" : "attention", state === AccessRequestState.PENDING ? `${request.validApprovals} / ${request.requiredApprovals} valid approvals` : "Rejected");
    if (state === AccessRequestState.REJECTED) { set(2, "not-run", "Not approved"); set(3, "not-run", "Not executed"); }
    return { nodes, notice };
  }
  set(1, request.requiredApprovals === 0 ? "automatic" : "complete", request.requiredApprovals === 0 ? "Automatic approval" : "Approval gate passed");
  set(2, state === AccessRequestState.APPROVED ? "current" : "complete", state === AccessRequestState.APPROVED ? "Awaiting requester" : "Execution admitted");
  if (state === AccessRequestState.APPROVED) return { nodes, notice };
  set(3, state === AccessRequestState.EXECUTING ? "current" : state === AccessRequestState.SUCCEEDED ? "complete" : "attention", stateLabel(state));
  if (state === AccessRequestState.OUTCOME_UNKNOWN) return { nodes, notice: "Outcome unknown. Check the target and audit history; this execution will not retry." };
  if (state === AccessRequestState.SUCCEEDED) return { nodes, notice: "Execution succeeded. Cached results may expire or be evicted; check result availability in details." };
  return { nodes, notice };
}
