import { describe, expect, it } from "vitest";

import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { actorLabel, hasLiveRequests, isExecutionOutcomeState, isLiveRequestState, requestStateFilterOptions, shouldPollRequestList } from "@/entities/request/model";

describe("actor label", () => {
  it("prefers the display name", () => {
    expect(actorLabel({ displayName: "Dana", email: "dana@example.com" })).toBe("Dana");
  });

  it("falls back to the email when the display name is empty", () => {
    expect(actorLabel({ displayName: "", email: "dana@example.com" })).toBe("dana@example.com");
  });

  it("shows a dash for an unknown actor", () => {
    expect(actorLabel(undefined)).toBe("—");
  });

  it("shows nothing invented when both fields are empty", () => {
    expect(actorLabel({ displayName: "", email: "" })).toBe("");
  });
});

// Background polling only pays off while a request can still change; finished requests never move again (PRD §4.4).
describe("live request states", () => {
  it.each([
    ["draft", AccessRequestState.DRAFT],
    ["pending", AccessRequestState.PENDING],
    ["approved", AccessRequestState.APPROVED],
    ["executing", AccessRequestState.EXECUTING],
  ])("treats %s as live", (_name, state) => {
    expect(isLiveRequestState(state)).toBe(true);
  });

  it.each([
    ["rejected", AccessRequestState.REJECTED],
    ["expired", AccessRequestState.EXPIRED],
    ["cancelled", AccessRequestState.CANCELLED],
    ["succeeded", AccessRequestState.SUCCEEDED],
    ["failed", AccessRequestState.FAILED],
    ["outcome unknown", AccessRequestState.OUTCOME_UNKNOWN],
  ])("treats %s as finished", (_name, state) => {
    expect(isLiveRequestState(state)).toBe(false);
  });

  it("polls a page that mixes finished and live requests", () => {
    expect(hasLiveRequests([{ effectiveState: AccessRequestState.SUCCEEDED }, { effectiveState: AccessRequestState.PENDING }])).toBe(true);
  });

  it("stops polling a page of finished requests", () => {
    expect(hasLiveRequests([{ effectiveState: AccessRequestState.SUCCEEDED }, { effectiveState: AccessRequestState.REJECTED }])).toBe(false);
  });

  it("does not poll an empty page", () => {
    expect(hasLiveRequests([])).toBe(false);
  });
});

// The list polls to show newly submitted requests too, and the newest-first first page is where they arrive (PRD §7.4).
describe("request list polling", () => {
  const finished = { effectiveState: AccessRequestState.SUCCEEDED };
  const pending = { effectiveState: AccessRequestState.PENDING };
  it.each([
    ["the first page of finished requests, where new requests arrive", [finished], 1],
    ["an empty first page awaiting the first request", [], 1],
    ["a later page that still shows a pending request", [finished, pending], 3],
    ["the first page with a pending request", [pending], 1],
  ])("polls %s", (_name, rows, page) => {
    expect(shouldPollRequestList(rows, page)).toBe(true);
  });
  it.each([
    ["a later page of finished requests", [finished, finished], 2],
    ["a later empty page", [], 4],
    ["a later page of rejected and expired requests", [{ effectiveState: AccessRequestState.REJECTED }, { effectiveState: AccessRequestState.EXPIRED }], 2],
    ["a later page of cancelled and unknown outcomes", [{ effectiveState: AccessRequestState.CANCELLED }, { effectiveState: AccessRequestState.OUTCOME_UNKNOWN }], 5],
  ])("does not poll %s", (_name, rows, page) => {
    expect(shouldPollRequestList(rows, page)).toBe(false);
  });
});

describe("execution outcome states", () => {
  it.each([AccessRequestState.SUCCEEDED, AccessRequestState.FAILED, AccessRequestState.OUTCOME_UNKNOWN])("recognizes outcome state %i", (state) => {
    expect(isExecutionOutcomeState(state)).toBe(true);
  });

  it.each([AccessRequestState.APPROVED, AccessRequestState.EXECUTING, AccessRequestState.CANCELLED, AccessRequestState.UNSPECIFIED])("refuses non-outcome state %i", (state) => {
    expect(isExecutionOutcomeState(state)).toBe(false);
  });
});

// The filter shows the same words as the state badges while sending the server's own vocabulary.
const labelOf = (value: string) => requestStateFilterOptions.find((option) => option.value === value)?.label;

describe("request state filter options", () => {
  it("labels a single-word state with its capitalized badge text", () => {
    expect(labelOf("pending")).toBe("Pending");
  });

  it("labels a two-word state as words, not as its wire identifier", () => {
    expect(labelOf("outcome_unknown")).toBe("Outcome unknown");
  });

  it("labels execution outcomes", () => {
    expect([labelOf("executing"), labelOf("succeeded"), labelOf("failed")]).toEqual(["Executing", "Succeeded", "Failed"]);
  });

  it("keeps the workflow order the filter has always used", () => {
    expect(requestStateFilterOptions.map((option) => option.value)).toEqual([
      "draft", "pending", "approved", "executing", "succeeded", "failed", "outcome_unknown", "rejected", "expired", "cancelled",
    ]);
  });

  it("never shows a raw wire identifier as a label", () => {
    for (const option of requestStateFilterOptions) {
      expect(option.label).not.toBe(option.value);
      expect(option.label).not.toContain("_");
    }
  });

  it("never falls back to the unknown-state label", () => {
    expect(requestStateFilterOptions.map((option) => option.label)).not.toContain("Unknown");
  });

  it("offers each state once", () => {
    const values = requestStateFilterOptions.map((option) => option.value);
    expect(new Set(values).size).toBe(values.length);
  });

  it("offers no option for an unsupported state", () => {
    expect(labelOf("archived")).toBeUndefined();
  });
});
