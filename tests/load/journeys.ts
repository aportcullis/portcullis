import { check } from 'k6';
import { z } from 'zod';

import { rpc, exportCSV } from './client.ts';
import type { Fixture, RPCs } from './contracts.ts';

export function browse(fixture: Fixture): void {
  rpc('Auth.Me', {}, fixture.requester);
  const list = rpc('AccessRequests.List', { page: 1, pageSize: 20 }, fixture.requester);
  if (!check(list, { 'page is bounded': (r) => r.items.length <= 20 })) {
    throw new Error('Invalid request page');
  }
  rpc('AccessRequests.ListRequestableConnections', {}, fixture.requester);
}

export function submit(fixture: Fixture, review: boolean): void {
  if (review && !fixture.approver) throw new Error('Review needs an approver');
  const created = rpc('AccessRequests.Create', {
    connectionId: fixture.connectionId, sql: 'SELECT :value AS value',
    params: [{ name: 'value', type: 'integer', value: '42' }],
  }, fixture.requester).request;
  try {
    const submitted = rpc('AccessRequests.Submit', {
      id: created.id, expectedVersion: created.version,
    }, fixture.requester).request;
    if (review && fixture.approver) {
      if (submitted.state !== 'ACCESS_REQUEST_STATE_PENDING' || submitted.requiredApprovals !== 1) {
        throw new Error('Review fixture must require exactly one approval');
      }
      rpc('AccessRequests.Get', { id: created.id }, fixture.approver);
      // Reopening the review page must preserve the same immutable request.
      rpc('AccessRequests.Get', { id: created.id }, fixture.approver);
      const approved = rpc('AccessRequests.Approve', { id: created.id }, fixture.approver).request;
      if (approved.state !== 'ACCESS_REQUEST_STATE_APPROVED') throw new Error('Approval did not transition');
    } else if (!['ACCESS_REQUEST_STATE_PENDING', 'ACCESS_REQUEST_STATE_APPROVED'].includes(submitted.state)) {
      throw new Error('Submit did not transition');
    }
    rpc('AccessRequests.Get', { id: created.id }, fixture.requester);
  } finally {
    const cancelled = rpc('AccessRequests.Cancel', { id: created.id }, fixture.requester).request;
    if (cancelled.state !== 'ACCESS_REQUEST_STATE_CANCELLED') throw new Error('Cancel did not transition');
  }
}

/** Executes a distinctly approved read, then verifies paging, sorting, filtering and streamed CSV. */
export function executeAndExplore(fixture: Fixture): void {
  if (!fixture.approver) throw new Error('Execution needs a distinct approver');
  const created = rpc('AccessRequests.Create', {
    connectionId: fixture.connectionId,
    sql: "SELECT (9007199254740993::bigint + id) AS exact_value, note FROM load_numbers WHERE id <= 25 ORDER BY id",
    params: [],
  }, fixture.requester).request;
  const submitted = rpc('AccessRequests.Submit', { id: created.id, expectedVersion: created.version }, fixture.requester).request;
  if (submitted.state !== 'ACCESS_REQUEST_STATE_PENDING' || submitted.requiredApprovals !== 1) throw new Error('Execution fixture must require one review');
  rpc('AccessRequests.Get', { id: created.id }, fixture.approver);
  const approved = rpc('AccessRequests.Approve', { id: created.id }, fixture.approver).request;
  if (approved.state !== 'ACCESS_REQUEST_STATE_APPROVED') throw new Error('Execution approval failed');
  const executed = rpc('QueryExecutions.Execute', { requestId: created.id }, fixture.requester);
  if (!check(executed, { 'approved read produces durable results': value => value.state === 'ACCESS_REQUEST_STATE_SUCCEEDED' && value.resultAvailable === true && value.rowCount === '25' && !value.truncated })) {
    throw new Error('Execution journey did not produce exact results');
  }
  const stored = rpc('QueryExecutions.Get', { requestId: created.id }, fixture.requester);
  if (stored.state !== executed.state) throw new Error('Durable execution state differs');
  const first = rpc('QueryExecutions.GetResult', { requestId: created.id, page: 1, pageSize: 20 }, fixture.requester);
  if (first.totalCount !== '25' || first.rows.length !== 20 || integerAt(first, 0) !== '9007199254740994') throw new Error('First page lost bounds or integer precision');
  const last = rpc('QueryExecutions.GetResult', { requestId: created.id, page: 2, pageSize: 20 }, fixture.requester);
  if (last.rows.length !== 5 || integerAt(last, 4) !== '9007199254741018') throw new Error('Second page is incorrect');
  const sorted = rpc('QueryExecutions.GetResult', { requestId: created.id, page: 1, pageSize: 20, sortColumn: 0, descending: true }, fixture.requester);
  if (integerAt(sorted, 0) !== '9007199254741018') throw new Error('Numeric sort lost exact values');
  const filtered = rpc('QueryExecutions.GetResult', { requestId: created.id, page: 1, pageSize: 20, filterColumn: 0, filter: '9007199254740994' }, fixture.requester);
  if (filtered.totalCount !== '1' || filtered.rows.length !== 1) throw new Error('Filtering produced incorrect rows');
  const csv = exportCSV(created.id, fixture.requester);
  if (!csv.includes("9007199254740994,'=formula") || !csv.includes("9007199254741018,'=formula") || csv.trim().split('\n').length !== 26) throw new Error('CSV lost rows, precision or formula escaping');
}

/** Requires the scenario's requested row and first integer cell before comparing exact values. */
function integerAt(page: RPCs['QueryExecutions.GetResult']['output'], index: number): string {
  const row = z.object({ cells: z.array(z.unknown()) }).parse(page.rows[index]);
  return z.strictObject({ intValue: z.string() }).parse(row.cells[0]).intValue;
}
