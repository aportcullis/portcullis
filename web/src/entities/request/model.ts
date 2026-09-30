import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";

// ParamType is the UI-known parameter-value vocabulary (PRD §4.2), mirroring the server's domain enum. A union so a typo fails to compile.
export type ParamType =
  | "string"
  | "integer"
  | "decimal"
  | "boolean"
  | "date"
  | "timestamp"
  | "uuid"
  | "null";

export const paramTypes: readonly ParamType[] = [
  "string",
  "integer",
  "decimal",
  "boolean",
  "date",
  "timestamp",
  "uuid",
  "null",
];

// ParamRow is one editable parameter in the create/edit form.
export type ParamRow = { name: string; type: ParamType; value: string };

// Badge variant names available in the vendored solid-ui Badge.
export type BadgeVariant = "default" | "secondary" | "destructive" | "outline";

// stateLabel renders a request state as human text.
export function stateLabel(state: AccessRequestState): string {
  switch (state) {
    case AccessRequestState.DRAFT:
      return "Draft";
    case AccessRequestState.PENDING:
      return "Pending";
    case AccessRequestState.APPROVED:
      return "Approved";
    case AccessRequestState.REJECTED:
      return "Rejected";
    case AccessRequestState.EXPIRED:
      return "Expired";
    case AccessRequestState.CANCELLED:
      return "Cancelled";
    case AccessRequestState.EXECUTING:
      return "Executing";
    case AccessRequestState.SUCCEEDED:
      return "Succeeded";
    case AccessRequestState.FAILED:
      return "Failed";
    case AccessRequestState.OUTCOME_UNKNOWN:
      return "Outcome unknown";
    default:
      return "Unknown";
  }
}

// stateBadge maps a state onto a Badge variant: approved/succeeded read as positive (default), pending as attention (secondary), the failure family as destructive, and the inert terminal states as muted (outline).
export function stateBadge(state: AccessRequestState): BadgeVariant {
  switch (state) {
    case AccessRequestState.APPROVED:
    case AccessRequestState.SUCCEEDED:
      return "default";
    case AccessRequestState.PENDING:
    case AccessRequestState.EXECUTING:
      return "secondary";
    case AccessRequestState.REJECTED:
    case AccessRequestState.FAILED:
    case AccessRequestState.OUTCOME_UNKNOWN:
      return "destructive";
    default:
      return "outline";
  }
}
