import { check } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate, Trend } from 'k6/metrics';

import { rpc, exportCSV } from '#load/client';
import { approveRead, cancelExecution } from '#load/execution-journey';
import { fixturesByVirtualUser, verifyFixtures } from '#load/fixtures';
import { reviewFixtureForVirtualUser } from '#load/fixture-assignment';

export const setup = verifyFixtures;
export const options = {
  scenarios: { limits: { executor: 'per-vu-iterations', vus: 1, iterations: 3, maxDuration: '2m' } },
  summaryTrendStats: ['avg', 'max', 'p(95)', 'p(99)'],
  thresholds: {
    checks: ['rate==1'], http_req_failed: ['rate==0'], journey_failed: ['rate==0'],
    journeys_completed: ['count==3'], control_plane_ms: ['p(95)<500'], cancel_settle_ms: ['p(95)<5000'],
  },
};
const completed = new Counter('journeys_completed');
const failed = new Rate('journey_failed');
const cancelDuration = new Trend('cancel_settle_ms', true);

/** Exercises bounded large results, wide rows and cancellation through the real governed API. */
export default async function (): Promise<void> {
  const fixture = reviewFixtureForVirtualUser(fixturesByVirtualUser, exec.vu.idInTest);
  try {
    const index = exec.vu.iterationInScenario;
    if (index === 2) {
      const id = await approveRead(fixture, 'SELECT sum(i) FROM generate_series(1,100000000) AS i');
      const cancelled = await cancelExecution(id, fixture.requester);
      cancelDuration.add(cancelled.settleMilliseconds);
    } else {
      const sql = index === 0 ? 'SELECT id FROM load_numbers ORDER BY id' : "SELECT repeat('x',65536) AS payload FROM generate_series(1,1000)";
      const id = await approveRead(fixture, sql);
      const result = rpc('QueryExecutions.Execute', { requestId: id }, fixture.requester);
      const rows = Number(result.rowCount);
      if (!check(result, { 'large results retain enforced row and byte limits': value => value.state === 'ACCESS_REQUEST_STATE_SUCCEEDED' && value.resultAvailable === true && value.truncated === true && rows > 0 && rows <= 10000 && Number(value.byteCount) <= 26214400 })) throw new Error('Result limits were not preserved');
      if (index === 0 && rows !== 10000) throw new Error('Row cap changed');
      if (index === 1 && rows >= 1000) throw new Error('Wide result did not reach byte cap');
      const page = rpc('QueryExecutions.GetResult', { requestId: id, page: 1, pageSize: 20 }, fixture.requester);
      if (page.rows.length !== 20 || Number(page.totalCount) !== rows || !page.truncated) throw new Error('Bounded snapshot paging is incorrect');
      if (index === 0 && exportCSV(id, fixture.requester).trim().split('\n').length !== 10001) throw new Error('Large CSV row count differs from snapshot');
    }
    completed.add(1);
    failed.add(false);
  } catch (error) {
    failed.add(true);
    throw error;
  }
}
