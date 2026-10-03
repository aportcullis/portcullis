import assert from 'node:assert/strict';
import { test } from 'node:test';
import { z } from 'zod';

import { createRPCRequest, decodeRPCResponse } from '../wire.ts';
import { decodeJSON, encodeJSON } from '../json.ts';
import { base64Text, fixtureSchema, rpcSchemas } from '../contracts.ts';

void test('method-specific request retains Unicode, escaping and exact int64 tokens', () => {
  const sql = 'select "한글", \'line\\path\'\nfrom source';
  const request = createRPCRequest('http://localhost:8080/', 'AccessRequests.Create', { connectionId: 'connection-1', sql, params: [] });
  assert.equal(request.url, 'http://localhost:8080/portcullis.v1.AccessRequests/Create');
  assert.equal(decodeJSON(rpcSchemas['AccessRequests.Create'].input, request.body).sql, sql);
  const submit = createRPCRequest('http://localhost:8080', 'AccessRequests.Submit', { id: 'request-1', expectedVersion: '9007199254740993' });
  assert.equal(decodeJSON(rpcSchemas['AccessRequests.Submit'].input, submit.body).expectedVersion, '9007199254740993');
  assert.equal(createRPCRequest('http://localhost:8080', 'Auth.Me', {}).body, '{}');
});

void test('invalid requests fail before reaching HTTP transport', () => {
  assert.throws(() => encodeJSON(rpcSchemas['QueryExecutions.Execute'].input, { requestId: '' }), /Invalid JSON contract/);
  assert.throws(() => decodeJSON(rpcSchemas['QueryExecutions.Execute'].input, '{"requestId":"ok","sql":"unsafe"}'), /Invalid JSON contract/);
  assert.throws(() => encodeJSON(rpcSchemas['AccessRequests.List'].input, { page: Number.NaN, pageSize: 20 }), /Invalid JSON contract/);
});

void test('execution response supplies validated ProtoJSON scalar defaults', () => {
  const response = decodeRPCResponse('QueryExecutions.Execute', '{"state":"ACCESS_REQUEST_STATE_SUCCEEDED","rowCount":"9007199254740993"}');
  assert.equal(response.rowCount, '9007199254740993');
  assert.equal(response.durationMs, '0');
  assert.equal(response.resultAvailable, false);
  assert.equal(response.truncated, false);
});

void test('malformed responses cannot masquerade as successful RPC output', () => {
  for (const body of ['{}', '{"state":"INVALID"}', '{"state":"ACCESS_REQUEST_STATE_SUCCEEDED","rowCount":42}', '{"state":"ACCESS_REQUEST_STATE_SUCCEEDED","durationMs":"oops"}']) {
    assert.throws(() => decodeRPCResponse('QueryExecutions.Execute', body), /Invalid JSON contract/);
  }
  assert.throws(() => decodeRPCResponse('AccessRequests.Create', '{"request":{"id":"r","state":"ACCESS_REQUEST_STATE_DRAFT"}}'), /Invalid JSON contract/);
});

void test('empty pages normalize arrays while invalid cells are rejected', () => {
  const empty = decodeRPCResponse('QueryExecutions.GetResult', '{}');
  assert.deepEqual(empty.rows, []);
  assert.deepEqual(empty.columns, []);
  assert.equal(empty.totalCount, '0');
  assert.throws(() => decodeRPCResponse('QueryExecutions.GetResult', '{"rows":[{"cells":[{"intValue":"1","stringValue":"two"}]}]}'), /Invalid JSON contract/);
  assert.throws(() => decodeRPCResponse('QueryExecutions.GetResult', '{"rows":[{"cells":[{"intValue":9007199254740993}]}]}'), /Invalid JSON contract/);
});

void test('invalid JSON and fixture shapes fail without disclosing secrets', () => {
  const secret = 'private-session-token';
  assert.throws(() => decodeJSON(z.array(fixtureSchema), '{"session":"'+secret+'"'), { message: 'Invalid JSON contract' });
  assert.throws(() => decodeJSON(z.array(fixtureSchema), '[{"connectionId":"c","requester":{"session":"'+secret+'","csrf":12}}]'), { message: 'Invalid JSON contract' });
});

void test('protobuf base64 validates bounded padding without browser globals', () => {
  for (const value of ['', 'SGVsbG8=', 'eA==', 'YWJj']) assert.equal(base64Text.safeParse(value).success, true);
  for (const value of ['x', 'invalid===', 'a b=', '=abc', 'abc_']) assert.equal(base64Text.safeParse(value).success, false);
});
