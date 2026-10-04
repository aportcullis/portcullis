import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import {
  ConnectionPolicySchema,
  GetConnectionPolicyResponseSchema,
} from "@/gen/portcullis/v1/connection_policies_pb";
import { requireReturnedPolicy } from "@/features/connection/policyRead";

// A GetPolicy answer without a policy cannot seed the form; treating it as a read failure gives the panel an error and a retry instead of an empty body.
const policy = (version: bigint, maxRows: number) =>
  create(ConnectionPolicySchema, { connectionId: "conn-1", version, maxRows, queryTimeoutSeconds: 30, maxResultBytes: 16n * 1048576n });

function readFailure(call: () => unknown): ConnectError | undefined {
  try {
    call();
  } catch (error: unknown) {
    return error instanceof ConnectError ? error : undefined;
  }
  return undefined;
}

describe("requireReturnedPolicy", () => {
  it("returns the policy of a full response", () => {
    const returned = policy(3n, 10000);
    expect(requireReturnedPolicy(create(GetConnectionPolicyResponseSchema, { policy: returned }))).toBe(returned);
  });

  it("returns a first-version policy", () => {
    expect(requireReturnedPolicy({ policy: policy(1n, 1) })?.version).toBe(1n);
  });

  it("returns a policy whose numeric fields are all zero", () => {
    expect(requireReturnedPolicy({ policy: create(ConnectionPolicySchema, {}) })?.maxRows).toBe(0);
  });

  it("returns the exact policy object, not a copy", () => {
    const returned = policy(9n, 50);
    expect(requireReturnedPolicy({ policy: returned })).toBe(returned);
  });

  it("refuses an empty response message as a read failure", () => {
    const failure = readFailure(() => requireReturnedPolicy(create(GetConnectionPolicyResponseSchema, {})));
    expect(failure?.code).toBe(Code.DataLoss);
  });

  it("refuses a response whose policy is explicitly undefined", () => {
    expect(readFailure(() => requireReturnedPolicy({ policy: undefined }))).toBeInstanceOf(ConnectError);
  });

  it("refuses a bare object with no policy field", () => {
    expect(readFailure(() => requireReturnedPolicy({}))).toBeInstanceOf(ConnectError);
  });

  it("explains the failure in words the panel can show", () => {
    expect(readFailure(() => requireReturnedPolicy({}))?.rawMessage).toBe("The server returned no policy for this connection.");
  });
});
