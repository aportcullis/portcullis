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
}

export type RPCName = keyof RPCs;
