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

// A finished execution's states; with rejection, expiry and cancellation they are terminal and never revisited (PRD §4.4).
const executionOutcomeStates: readonly AccessRequestState[] = [
  AccessRequestState.SUCCEEDED,
  AccessRequestState.FAILED,
  AccessRequestState.OUTCOME_UNKNOWN,
];

const terminalStates: readonly AccessRequestState[] = [
  AccessRequestState.REJECTED,
  AccessRequestState.EXPIRED,
  AccessRequestState.CANCELLED,
  ...executionOutcomeStates,
];

/** Reports whether a request in this state can still change, so a background refresh may show something new. */
export function isLiveRequestState(state: AccessRequestState): boolean {
  return !terminalStates.includes(state);
}

/** Reports whether any of the shown requests can still change. */
export function hasLiveRequests(requests: readonly { effectiveState: AccessRequestState }[]): boolean {
  return requests.some((request) => isLiveRequestState(request.effectiveState));
}

/** Reports whether the request list should poll: always on the newest-first first page, where new requests arrive, and elsewhere only while a shown request can still change. */
export function shouldPollRequestList(requests: readonly { effectiveState: AccessRequestState }[], page: number): boolean {
  return page === 1 || hasLiveRequests(requests);
}

/** Reports whether the state records a finished execution whose outcome and result the owner may inspect. */
export function isExecutionOutcomeState(state: AccessRequestState): boolean {
  return executionOutcomeStates.includes(state);
}

/** One option of the request list's state filter: the wire value the server filters on and its human label. */
export type RequestStateFilterOption = { value: string; label: string };

// The filter covers review and execution outcomes in workflow order; values are the List RPC's state vocabulary.
const filterStates: readonly { value: string; state: AccessRequestState }[] = [
  { value: "draft", state: AccessRequestState.DRAFT },
  { value: "pending", state: AccessRequestState.PENDING },
  { value: "approved", state: AccessRequestState.APPROVED },
  { value: "executing", state: AccessRequestState.EXECUTING },
  { value: "succeeded", state: AccessRequestState.SUCCEEDED },
  { value: "failed", state: AccessRequestState.FAILED },
  { value: "outcome_unknown", state: AccessRequestState.OUTCOME_UNKNOWN },
  { value: "rejected", state: AccessRequestState.REJECTED },
  { value: "expired", state: AccessRequestState.EXPIRED },
  { value: "cancelled", state: AccessRequestState.CANCELLED },
];

/** The state filter options with labels matching the state badges. */
export const requestStateFilterOptions: readonly RequestStateFilterOption[] = filterStates.map(({ value, state }) => ({
  value,
  label: stateLabel(state),
}));

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
