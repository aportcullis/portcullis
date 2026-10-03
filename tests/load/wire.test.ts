import { check } from 'k6';

import { createRPCRequest } from '#load/wire';
import { base64Text, rpcSchemas } from '#load/contracts';
import { decodeJSON } from '#load/json';

export const options = { vus: 1, iterations: 1, thresholds: { checks: ['rate==1'] } };

/** Checks method routing, JSON escaping and exact ProtoJSON version values. */
export default function (): void {
  check(null, { 'protobuf base64 validates in k6 without atob': () => base64Text.safeParse('SGVsbG8=').success && !base64Text.safeParse('invalid===').success });
  const execute = createRPCRequest('http://localhost:8080', 'QueryExecutions.Execute', { requestId: 'request-1' });
  check(execute, {
    'method chooses the Connect route': value => value.url === 'http://localhost:8080/portcullis.v1.QueryExecutions/Execute',
    'execution sends JSON instead of a form': value => value.body === '{"requestId":"request-1"}',
  });
  const sql = 'select "한글", \'line\\path\'\nfrom source';
  const create = createRPCRequest('http://localhost:8080/', 'AccessRequests.Create', { connectionId: 'connection-1', sql, params: [] });
  check(create, {
    'trailing slash does not change the route': value => value.url === 'http://localhost:8080/portcullis.v1.AccessRequests/Create',
    'SQL Unicode and escaping survive serialization': value => decodeJSON(rpcSchemas['AccessRequests.Create'].input, value.body).sql === sql,
  });
  const submit = createRPCRequest('http://localhost:8080', 'AccessRequests.Submit', { id: 'request-1', expectedVersion: '9007199254740993' });
  check(submit, { 'int64 versions retain exact decimal strings': value => value.body.includes('"expectedVersion":"9007199254740993"') });
  const me = createRPCRequest('http://localhost:8080', 'Auth.Me', {});
  check(me, { 'empty request remains a JSON object': value => value.body === '{}' });
}
