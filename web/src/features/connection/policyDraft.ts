import type { ConnectionPolicy } from "@/gen/portcullis/v1/connection_policies_pb";

// StatementClassKey mirrors the server's statement-class vocabulary.
export type StatementClassKey = "read" | "write" | "ddl";

export const STATEMENT_CLASSES: readonly { key: StatementClassKey; label: string }[] = [
  { key: "read", label: "Read" },
  { key: "write", label: "Write" },
  { key: "ddl", label: "DDL" },
];

export type ClassRuleDraft = { allowed: boolean; requiredApprovals: number };

// PolicyDraft is the form's shape of one policy version (ADR-0015): per-class gates plus one execution-limit set. expectedVersion pins the version the form was loaded from — the optimistic token the update sends back.
export type PolicyDraft = {
  expectedVersion: bigint;
  read: ClassRuleDraft;
  write: ClassRuleDraft;
  ddl: ClassRuleDraft;
  queryTimeoutSeconds: number;
  maxRows: number;
  maxResultBytes: bigint;
};

// Bounds mirror the server's CHECK constraints (ADR-0015); the server stays authoritative — these exist for inline form validation only.
export const POLICY_BOUNDS = {
  approvals: { min: 0, max: 100 },
  timeoutSeconds: { min: 1, max: 300 },
  rows: { min: 1, max: 10_000 },
  bytes: { min: 4096n, max: 67_108_864n },
} as const;

export function fromPolicy(p: ConnectionPolicy): PolicyDraft {
  return {
    expectedVersion: p.version,
    read: { allowed: p.read?.allowed ?? false, requiredApprovals: p.read?.requiredApprovals ?? 1 },
    write: { allowed: p.write?.allowed ?? false, requiredApprovals: p.write?.requiredApprovals ?? 1 },
    ddl: { allowed: p.ddl?.allowed ?? false, requiredApprovals: p.ddl?.requiredApprovals ?? 1 },
    queryTimeoutSeconds: p.queryTimeoutSeconds,
    maxRows: p.maxRows,
    maxResultBytes: p.maxResultBytes,
  };
}

export function rule(draft: PolicyDraft, key: StatementClassKey): ClassRuleDraft {
  return draft[key];
}

// newlyEnabledClasses lists risk classes the draft turns ON relative to the loaded policy — the trigger for the destructive warning (enabling write/DDL is an audited admin decision, PRD §4.3). Read is not a risk class.
export function newlyEnabledClasses(loaded: PolicyDraft, draft: PolicyDraft): StatementClassKey[] {
  return (["write", "ddl"] as const).filter((key) => !loaded[key].allowed && draft[key].allowed);
}

// autoApproveClasses lists enabled classes with a zero quorum — submit is allowed (§4.3 small-team deadlock relief) but the form surfaces it.
export function autoApproveClasses(draft: PolicyDraft): StatementClassKey[] {
  return (["read", "write", "ddl"] as const).filter(
    (key) => draft[key].allowed && draft[key].requiredApprovals === 0,
  );
}

// validate returns the first human-readable problem, or "" when the draft is inside the ADR-0015 bounds.
export function validate(draft: PolicyDraft): string {
  for (const { key, label } of STATEMENT_CLASSES) {
    const approvals = draft[key].requiredApprovals;
    if (
      !Number.isInteger(approvals) ||
      approvals < POLICY_BOUNDS.approvals.min ||
      approvals > POLICY_BOUNDS.approvals.max
    ) {
      return `${label} approvals must be a whole number between ${POLICY_BOUNDS.approvals.min} and ${POLICY_BOUNDS.approvals.max}.`;
    }
  }
  if (
    !Number.isInteger(draft.queryTimeoutSeconds) ||
    draft.queryTimeoutSeconds < POLICY_BOUNDS.timeoutSeconds.min ||
    draft.queryTimeoutSeconds > POLICY_BOUNDS.timeoutSeconds.max
  ) {
    return `Query timeout must be between ${POLICY_BOUNDS.timeoutSeconds.min} and ${POLICY_BOUNDS.timeoutSeconds.max} seconds.`;
  }
  if (
    !Number.isInteger(draft.maxRows) ||
    draft.maxRows < POLICY_BOUNDS.rows.min ||
    draft.maxRows > POLICY_BOUNDS.rows.max
  ) {
    return `Max rows must be between ${POLICY_BOUNDS.rows.min} and ${POLICY_BOUNDS.rows.max}.`;
  }
  if (draft.maxResultBytes < POLICY_BOUNDS.bytes.min || draft.maxResultBytes > POLICY_BOUNDS.bytes.max) {
    return "Max result size must be between 4 KiB and 64 MiB.";
  }
  return "";
}
