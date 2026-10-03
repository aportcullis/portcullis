export interface Actor {
  session: string;
  csrf: string;
}

export interface Fixture {
  connectionId: string;
  requester: Actor;
  approver?: Actor;
}

interface RequestView {
  id: string;
  version: string;
  state: string;
  requiredApprovals?: number;
}

interface RequestResponse {
  request: RequestView;
}

interface RequestID {
  id: string;
}

interface ExecutionView {
  state: string;
  rowCount?: string;
  byteCount?: string;
  durationMs?: string;
  resultAvailable?: boolean;
  truncated?: boolean;
}

export interface RPCs {
  'Auth.Me': {
    input: Record<string, never>;
    output: { user: { id: string } };
  };
  'AccessRequests.List': {
    input: { page: number; pageSize: number };
    output: { items?: RequestView[] };
  };
  'AccessRequests.ListRequestableConnections': {
    input: Record<string, never>;
    output: { connections?: { id: string }[] };
  };
  'AccessRequests.Create': {
    input: { connectionId: string; sql: string; params: { name: string; type: string; value: string }[] };
    output: RequestResponse;
  };
  'AccessRequests.Submit': {
    input: RequestID & { expectedVersion: string };
    output: RequestResponse;
  };
  'AccessRequests.Get': { input: RequestID; output: RequestResponse };
  'AccessRequests.Approve': { input: RequestID; output: RequestResponse };
  'AccessRequests.Cancel': { input: RequestID; output: RequestResponse };
  'QueryExecutions.Execute': { input: { requestId: string }; output: ExecutionView };
  'QueryExecutions.Get': { input: { requestId: string }; output: ExecutionView };
  'QueryExecutions.Cancel': { input: { requestId: string }; output: Record<string, never> };
  'QueryExecutions.GetResult': {
    input: { requestId: string; page: number; pageSize: number; sortColumn?: number; descending?: boolean; filterColumn?: number; filter?: string };
    output: { totalCount: string; truncated?: boolean; rows?: { cells: { intValue?: string; stringValue?: string }[] }[] };
  };
}

export type RPCName = keyof RPCs;
