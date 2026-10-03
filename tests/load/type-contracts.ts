import { rpc, rpcAsync } from '#load/client';
import { createRPCRequest, decodeRPCResponse } from '#load/wire';
import type { Actor, RPCs } from '#load/contracts';

/** Compile-only scenarios require method-specific inputs and inferred response types. */
export function verifyRPCTypeContracts(actor: Actor): void {
  const pending: Promise<RPCs['QueryExecutions.Execute']['output']> = rpcAsync('QueryExecutions.Execute', { requestId: 'r' }, actor);
  void pending;
  // @ts-expect-error Execute accepts requestId, not the request-edit id field.
  rpcAsync('QueryExecutions.Execute', { id: 'r' }, actor);
  // @ts-expect-error ProtoJSON int64 versions must remain decimal strings.
  rpc('AccessRequests.Submit', { id: 'r', expectedVersion: 1 }, actor);
  // @ts-expect-error The method name determines the allowed payload.
  createRPCRequest('http://localhost:8080', 'Auth.Me', { requestId: 'r' });
  const execution = decodeRPCResponse('QueryExecutions.Execute', '{"state":"ACCESS_REQUEST_STATE_SUCCEEDED"}');
  // @ts-expect-error Execution does not return an access-request response.
  void execution.request;
  const page = decodeRPCResponse('QueryExecutions.GetResult', '{}');
  const count: number = page.rows.length;
  void count;
}
