import { describe, expect, it } from "vitest";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { requestWorkflow } from "@/features/request/workflow";

describe("request progress from server facts", () => {
  it("shows a pending quorum without claiming approval or execution", () => {
    const flow = requestWorkflow({ effectiveState: AccessRequestState.PENDING, requiredApprovals: 2, validApprovals: 1 });
    expect(flow.nodes.map(node => node.status)).toEqual(["complete", "current", "upcoming", "upcoming"]);
    expect(flow.nodes[1].detail).toBe("1 / 2 valid approvals");
  });
  it("distinguishes automatic approval from human review", () => {
    const flow = requestWorkflow({ effectiveState: AccessRequestState.APPROVED, requiredApprovals: 0, validApprovals: 0 });
    expect(flow.nodes[1].status).toBe("automatic");
    expect(flow.nodes[2].status).toBe("current");
    expect(flow.nodes[3].status).toBe("upcoming");
  });
  it.each([AccessRequestState.EXPIRED, AccessRequestState.CANCELLED])("does not invent the last active stage for a stopped request (%s)", state => {
    const flow = requestWorkflow({ effectiveState: state, requiredApprovals: 1, validApprovals: 0 });
    expect(flow.nodes.map(node => node.status)).toEqual(["complete", "unknown", "unknown", "not-run"]);
    expect(flow.notice).toContain("Last active stage is not available");
  });
  it("keeps unknown outcomes separate from success and refuses a retry implication", () => {
    const flow = requestWorkflow({ effectiveState: AccessRequestState.OUTCOME_UNKNOWN, requiredApprovals: 1, validApprovals: 1 });
    expect(flow.nodes[3].status).toBe("attention");
    expect(flow.nodes[3].detail).toBe("Outcome unknown");
    expect(flow.notice).toContain("will not retry");
  });
  it("labels success without promising that cached results remain available", () => {
    const flow = requestWorkflow({ effectiveState: AccessRequestState.SUCCEEDED, requiredApprovals: 1, validApprovals: 1 });
    expect(flow.nodes[3].detail).toBe("Succeeded");
    expect(flow.notice).toContain("may expire");
  });
  it("keeps an unknown server state uncertain", () => {
    const flow = requestWorkflow({ effectiveState: AccessRequestState.UNSPECIFIED, requiredApprovals: 0, validApprovals: 0 });
    expect(flow.nodes.every(node => node.status === "unknown")).toBe(true);
  });
});
