import { describe, expect, it } from "vitest";

import type { PolicyDraft } from "@/features/connection/policyDraft";
import {
  autoApproveClasses,
  newlyEnabledClasses,
  validate,
} from "@/features/connection/policyDraft";

const draft = (over: Partial<PolicyDraft> = {}): PolicyDraft => ({
  expectedVersion: 1n,
  read: { allowed: true, requiredApprovals: 1 },
  write: { allowed: false, requiredApprovals: 1 },
  ddl: { allowed: false, requiredApprovals: 1 },
  queryTimeoutSeconds: 30,
  maxRows: 10_000,
  maxResultBytes: 16n * 1048576n,
  ...over,
});

describe("newlyEnabledClasses", () => {
  it("lists only risk classes the draft turns ON relative to the loaded policy", () => {
    const loaded = draft();
    expect(newlyEnabledClasses(loaded, draft())).toEqual([]);
    expect(
      newlyEnabledClasses(loaded, draft({ write: { allowed: true, requiredApprovals: 2 } })),
    ).toEqual(["write"]);
    expect(
      newlyEnabledClasses(
        loaded,
        draft({
          write: { allowed: true, requiredApprovals: 2 },
          ddl: { allowed: true, requiredApprovals: 3 },
        }),
      ),
    ).toEqual(["write", "ddl"]);
  });

  it("does not warn for a class that stays enabled or gets disabled", () => {
    const writeOn = draft({ write: { allowed: true, requiredApprovals: 2 } });
    expect(newlyEnabledClasses(writeOn, writeOn)).toEqual([]);
    expect(newlyEnabledClasses(writeOn, draft())).toEqual([]);
  });
});

describe("autoApproveClasses", () => {
  it("lists enabled classes with a zero quorum only", () => {
    expect(autoApproveClasses(draft())).toEqual([]);
    expect(autoApproveClasses(draft({ read: { allowed: true, requiredApprovals: 0 } }))).toEqual([
      "read",
    ]);
    // A disabled class with 0 approvals is not auto-approving anything.
    expect(autoApproveClasses(draft({ write: { allowed: false, requiredApprovals: 0 } }))).toEqual(
      [],
    );
  });
});

describe("validate", () => {
  it("accepts the ADR-0015 boundary values", () => {
    expect(validate(draft())).toBe("");
    expect(
      validate(
        draft({
          read: { allowed: true, requiredApprovals: 0 },
          write: { allowed: true, requiredApprovals: 100 },
          queryTimeoutSeconds: 300,
          maxRows: 1,
          maxResultBytes: 4096n,
        }),
      ),
    ).toBe("");
    expect(validate(draft({ queryTimeoutSeconds: 1, maxResultBytes: 64n * 1048576n }))).toBe("");
  });

  it("rejects out-of-bounds values with a field-specific message", () => {
    expect(validate(draft({ read: { allowed: true, requiredApprovals: -1 } }))).toMatch(
      /Read approvals/,
    );
    expect(validate(draft({ ddl: { allowed: false, requiredApprovals: 101 } }))).toMatch(
      /DDL approvals/,
    );
    expect(validate(draft({ queryTimeoutSeconds: 0 }))).toMatch(/Query timeout/);
    expect(validate(draft({ queryTimeoutSeconds: 301 }))).toMatch(/Query timeout/);
    expect(validate(draft({ maxRows: 0 }))).toMatch(/Max rows/);
    expect(validate(draft({ maxRows: 10_001 }))).toMatch(/Max rows/);
    expect(validate(draft({ maxResultBytes: 4095n }))).toMatch(/Max result size/);
    expect(validate(draft({ maxResultBytes: 64n * 1048576n + 1n }))).toMatch(/Max result size/);
    // NaN from an empty number input must not pass as "in bounds".
    expect(validate(draft({ maxRows: Number.NaN }))).toMatch(/Max rows/);
  });
});
