import type { ClassRuleDraft, PolicyDraft } from "@/features/connection/policyDraft";

// Rebase each policy field against the original form: keep local-only edits and accept remote changes, including conflicts. Report conflicts so the admin can reconsider them.
export type PolicyRebase = { merged: PolicyDraft; conflicts: string[] };

// The field labels are what the admin is told; they read as the form's own words rather than as property paths.
const CLASS_LABELS = { read: "read", write: "write", ddl: "DDL" } as const;

export function rebasePolicy(base: PolicyDraft, mine: PolicyDraft, fresh: PolicyDraft): PolicyRebase {
  const conflicts: string[] = [];

  // resolveField decides one field. Values are compared with ===, which is exact for the primitives a policy is made of (boolean, number, bigint).
  const resolveField = <T extends boolean | number | bigint>(
    label: string,
    was: T,
    ours: T,
    theirs: T,
  ): T => {
    if (ours === was) return theirs;
    if (theirs === was) return ours;
    if (ours !== theirs) conflicts.push(label);
    return theirs;
  };

  const rule = (key: keyof typeof CLASS_LABELS): ClassRuleDraft => ({
    allowed: resolveField(
      `${CLASS_LABELS[key]} allowed`,
      base[key].allowed,
      mine[key].allowed,
      fresh[key].allowed,
    ),
    requiredApprovals: resolveField(
      `${CLASS_LABELS[key]} approvals`,
      base[key].requiredApprovals,
      mine[key].requiredApprovals,
      fresh[key].requiredApprovals,
    ),
  });

  const merged: PolicyDraft = {
    // The retry is guarded by the version we just read, not the one the form was built from — that is what makes it a retry rather than a second conflict.
    expectedVersion: fresh.expectedVersion,
    read: rule("read"),
    write: rule("write"),
    ddl: rule("ddl"),
    queryTimeoutSeconds: resolveField(
      "query timeout",
      base.queryTimeoutSeconds,
      mine.queryTimeoutSeconds,
      fresh.queryTimeoutSeconds,
    ),
    maxRows: resolveField("max rows", base.maxRows, mine.maxRows, fresh.maxRows),
    maxResultBytes: resolveField(
      "max result bytes",
      base.maxResultBytes,
      mine.maxResultBytes,
      fresh.maxResultBytes,
    ),
  };
  return { merged, conflicts };
}
