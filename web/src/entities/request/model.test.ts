import { describe, expect, it } from "vitest";

import { requestStateFilterOptions } from "@/entities/request/model";

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
