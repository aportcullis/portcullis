import { check } from 'k6';

import { rpc } from './client.ts';
import type { Fixture } from './contracts.ts';

export function browse(fixture: Fixture): void {
  rpc('Auth.Me', {}, fixture.requester);
  const list = rpc('AccessRequests.List', { page: 1, pageSize: 20 }, fixture.requester);
  if (!check(list, { 'page is bounded': (r) => (r.items || []).length <= 20 })) {
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
  if (!created?.id || !created.version) throw new Error('Create did not return an id/version');
  try {
    const submitted = rpc('AccessRequests.Submit', {
      id: created.id, expectedVersion: created.version,
    }, fixture.requester).request;
    if (review && fixture.approver) {
      if (submitted.state !== 'ACCESS_REQUEST_STATE_PENDING' || submitted.requiredApprovals !== 1) {
        throw new Error('Review fixture must require exactly one approval');
      }
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
