import { check, sleep } from 'k6';
import exec from 'k6/execution';

import { rpcAsync } from './client.ts';
import { fixturesByVirtualUser, verifyFixtures } from './fixtures.ts';
import { reviewFixtureForVirtualUser } from './fixture-assignment.ts';

export const setup = verifyFixtures;
export const options = {
  vus: 1, iterations: 1,
  thresholds: { checks: ['rate==1'], http_req_failed: ['rate==0'] },
};

/** Verifies typed async execution and the conservative cancellation contract against the real server. */
export default async function (): Promise<void> {
  const fixture = reviewFixtureForVirtualUser(fixturesByVirtualUser, exec.vu.idInTest);
  const created = await rpcAsync('AccessRequests.Create', {
    connectionId: fixture.connectionId,
    sql: 'SELECT sum(i) FROM generate_series(1,100000000) AS i', params: [],
  }, fixture.requester);
  await rpcAsync('AccessRequests.Submit', { id: created.request.id, expectedVersion: created.request.version }, fixture.requester);
  await rpcAsync('AccessRequests.Approve', { id: created.request.id }, fixture.approver);
  const request = { requestId: created.request.id };
  const pending = rpcAsync('QueryExecutions.Execute', request, fixture.requester);
  let active = false;
  for (let poll = 0; poll < 20; poll++) {
    const status = await rpcAsync('AccessRequests.Get', { id: request.requestId }, fixture.requester);
    if (status.request.state === 'ACCESS_REQUEST_STATE_EXECUTING') { active = true; break; }
    sleep(0.1);
  }
  if (!active) { await pending; throw new Error('Execution lease was not observable'); }
  await rpcAsync('QueryExecutions.Cancel', request, fixture.requester);
  const result = await pending;
  const saved = await rpcAsync('QueryExecutions.Get', request, fixture.requester);
  if (!check(result, {
    'cancelled target has no successful result snapshot': value => value.state === 'ACCESS_REQUEST_STATE_OUTCOME_UNKNOWN' && !value.resultAvailable,
    'conservative cancellation outcome is durable': value => value.state === saved.state && !saved.resultAvailable,
  })) throw new Error('Async cancellation contract failed');
}
