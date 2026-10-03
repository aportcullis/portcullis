import { check, sleep } from 'k6';

import { rpcAsync } from '#load/client';
import type { Actor, RPCs } from '#load/contracts';
import type { ReviewFixture } from '#load/fixture-assignment';

/** Creates, submits and approves one read using separate synthetic actors. */
export async function approveRead(fixture: ReviewFixture, sql: string): Promise<string> {
  const created = await rpcAsync('AccessRequests.Create', { connectionId: fixture.connectionId, sql, params: [] }, fixture.requester);
  const submitted = await rpcAsync('AccessRequests.Submit', { id: created.request.id, expectedVersion: created.request.version }, fixture.requester);
  if (submitted.request.state !== 'ACCESS_REQUEST_STATE_PENDING' || submitted.request.requiredApprovals !== 1) throw new Error('Unexpected approval policy');
  const approved = await rpcAsync('AccessRequests.Approve', { id: created.request.id }, fixture.approver);
  if (approved.request.state !== 'ACCESS_REQUEST_STATE_APPROVED') throw new Error('Review did not approve');
  return created.request.id;
}

/** Observes the active lease, cancels it and verifies the durable conservative outcome. */
export async function cancelExecution(requestID: string, requester: Actor): Promise<{ result: RPCs['QueryExecutions.Execute']['output']; settleMilliseconds: number }> {
  const request = { requestId: requestID };
  const pending = rpcAsync('QueryExecutions.Execute', request, requester);
  let active = false;
  for (let poll = 0; poll < 20; poll++) {
    const status = await rpcAsync('AccessRequests.Get', { id: requestID }, requester);
    if (status.request.state === 'ACCESS_REQUEST_STATE_EXECUTING') { active = true; break; }
    sleep(0.1);
  }
  if (!active) { await pending; throw new Error('Execution lease was not observable'); }
  const started = Date.now();
  await rpcAsync('QueryExecutions.Cancel', request, requester);
  const result = await pending;
  const settleMilliseconds = Date.now() - started;
  const saved = await rpcAsync('QueryExecutions.Get', request, requester);
  if (!check(result, {
    'cancelled target has no successful result snapshot': value => value.state === 'ACCESS_REQUEST_STATE_OUTCOME_UNKNOWN' && !value.resultAvailable,
    'conservative cancellation outcome is durable': value => value.state === saved.state && !saved.resultAvailable,
  })) throw new Error('Cancellation contract failed');
  return { result, settleMilliseconds };
}
