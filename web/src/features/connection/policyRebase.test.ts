import { describe, expect, it } from "vitest";

import type { PolicyDraft } from "@/features/connection/policyDraft";
import { rebasePolicy } from "@/features/connection/policyRebase";

const base = (): PolicyDraft => ({
  expectedVersion: 4n,
  read: { allowed: true, requiredApprovals: 1 },
  write: { allowed: true, requiredApprovals: 1 },
  ddl: { allowed: false, requiredApprovals: 1 },
  queryTimeoutSeconds: 60,
  maxRows: 500,
  maxResultBytes: 1_048_576n,
});

const withVersion = (draft: PolicyDraft, version: bigint): PolicyDraft => ({
  ...draft,
  expectedVersion: version,
});


describe("rebasePolicy", () => {
  it("keeps both edits when they touched different fields", () => {
    const mine = { ...base(), maxRows: 900 };
    const fresh = withVersion({ ...base(), write: { allowed: true, requiredApprovals: 2 } }, 5n);

    const { merged, conflicts } = rebasePolicy(base(), mine, fresh);

    expect(merged.maxRows).toBe(900);
    expect(merged.write.requiredApprovals).toBe(2);
    expect(merged.expectedVersion).toBe(5n);
    expect(conflicts).toEqual([]);
  });

  it("takes the other admin's value when both changed the same field, and names it", () => {
    const mine = { ...base(), write: { allowed: true, requiredApprovals: 3 } };
    const fresh = withVersion({ ...base(), write: { allowed: true, requiredApprovals: 2 } }, 5n);

    const { merged, conflicts } = rebasePolicy(base(), mine, fresh);

    expect(merged.write.requiredApprovals).toBe(2);
    expect(conflicts).toEqual(["write approvals"]);
  });

  it("adversarial: a stale draft cannot re-open a class the other admin just closed", () => {

    const mine = base();
    const fresh = withVersion({ ...base(), write: { allowed: false, requiredApprovals: 1 } }, 5n);

    const { merged, conflicts } = rebasePolicy(base(), mine, fresh);

    expect(merged.write.allowed).toBe(false);
    expect(conflicts).toEqual([]);
  });

  it("reports every field both sides changed", () => {
    const mine = {
      ...base(),
      read: { allowed: true, requiredApprovals: 2 },

      ddl: { allowed: true, requiredApprovals: 3 },
      queryTimeoutSeconds: 90,
      maxResultBytes: 2_097_152n,
    };
    const fresh = withVersion(
      {
        ...base(),
        read: { allowed: true, requiredApprovals: 3 },
        ddl: { allowed: true, requiredApprovals: 2 },
        queryTimeoutSeconds: 120,
        maxResultBytes: 4_194_304n,
      },
      5n,
    );

    const { merged, conflicts } = rebasePolicy(base(), mine, fresh);

    expect(merged.read.requiredApprovals).toBe(3);
    expect(merged.queryTimeoutSeconds).toBe(120);
    expect(merged.maxResultBytes).toBe(4_194_304n);
    expect(conflicts).toEqual([
      "read approvals",
      "DDL approvals",
      "query timeout",
      "max result bytes",
    ]);
  });

  it("returns the fresh policy when I changed nothing", () => {
    const fresh = withVersion({ ...base(), maxRows: 10 }, 5n);
    const { merged, conflicts } = rebasePolicy(base(), base(), fresh);
    expect(merged).toEqual(fresh);
    expect(conflicts).toEqual([]);
  });

  it("keeps my draft when the other side changed nothing but the version", () => {

    const mine = { ...base(), maxRows: 900, ddl: { allowed: true, requiredApprovals: 2 } };
    const fresh = withVersion(base(), 5n);

    const { merged, conflicts } = rebasePolicy(base(), mine, fresh);

    expect(merged).toEqual(withVersion(mine, 5n));
    expect(conflicts).toEqual([]);
  });
});
