import { Code, ConnectError } from "@connectrpc/connect";

import type { ConnectionPolicy } from "@/gen/portcullis/v1/connection_policies_pb";

/** Returns the policy carried by a GetPolicy response, failing the read when the server omitted it. */
export function requireReturnedPolicy(response: { policy?: ConnectionPolicy }): ConnectionPolicy {
  if (response.policy === undefined) {
    throw new ConnectError("The server returned no policy for this connection.", Code.DataLoss);
  }
  return response.policy;
}
