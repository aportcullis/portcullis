import exec from 'k6/execution';

import { approveRead, cancelExecution } from '#load/execution-journey';
import { fixturesByVirtualUser, verifyFixtures } from '#load/fixtures';
import { reviewFixtureForVirtualUser } from '#load/fixture-assignment';

export const setup = verifyFixtures;
export const options = {
  vus: 1, iterations: 1,
  thresholds: { checks: ['rate==1'], http_req_failed: ['rate==0'] },
};

/** Verifies typed async execution and conservative cancellation against the real server. */
export default async function (): Promise<void> {
  const fixture = reviewFixtureForVirtualUser(fixturesByVirtualUser, exec.vu.idInTest);
  const requestID = await approveRead(fixture, 'SELECT sum(i) FROM generate_series(1,100000000) AS i');
  await cancelExecution(requestID, fixture.requester);
}
