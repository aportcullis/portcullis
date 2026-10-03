import http, { type RefinedParams, type RefinedResponse } from 'k6/http';
import encoding from 'k6/encoding';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

import { config } from './config.ts';
import { base64Text, executionRequest, executionView, type Actor, type RPCName, type RPCs } from './contracts.ts';
import { createRPCRequest, decodeRPCResponse } from './wire.ts';
import { decodeJSON, encodeJSON } from './json.ts';
import { z } from 'zod';

const latency = new Trend('control_plane_ms', true);
const executionLatency = new Trend('execution_ms', true);
const targetDuration = new Trend('server_execution_ms', true);
const samples = new Counter('rpc_samples');
const throttled = new Counter('throttled_requests');

function requestParams(name: RPCName, actor: Actor, phase: 'setup' | 'workload'): RefinedParams<'text'> {
  return {
    headers: {
      'Content-Type': 'application/json',
      'Connect-Protocol-Version': '1',
      Cookie: `__Host-portcullis_session=${actor.session}; __Host-portcullis_csrf=${actor.csrf}`,
      'X-CSRF-Token': actor.csrf,
    },
    tags: { name, phase },
    timeout: name === 'QueryExecutions.Execute' ? '40s' : '10s',
    redirects: 0,
    responseType: 'text',
  };
}

function rpcResponse<K extends RPCName>(name: K, response: RefinedResponse<'text'>, phase: 'setup' | 'workload'): RPCs[K]['output'] {
  if (phase === 'workload') {
    samples.add(1, { rpc: name });
    if (name === 'QueryExecutions.Execute') executionLatency.add(response.timings.duration);
    else latency.add(response.timings.duration, { rpc: name });
    if (response.status === 429) throttled.add(1);
  }
  if (!check(response, { 'RPC succeeds': (r) => r.status === 200 })) {
    throw new Error(`${name} returned HTTP ${response.status}`);
  }
  const result = decodeRPCResponse(name, response.body);
  if (name === 'QueryExecutions.Execute') targetDuration.add(Number(executionView.parse(result).durationMs));
  return result;
}

/** Calls a governed RPC and records its transport and semantic outcome. */
export function rpc<K extends RPCName>(name: K, body: RPCs[NoInfer<K>]['input'], actor: Actor, phase: 'setup' | 'workload' = 'workload'): RPCs[K]['output'] {
  const request = createRPCRequest(config.baseURL, name, body);
  const response = http.post(request.url, request.body, requestParams(name, actor, phase));
  return rpcResponse(name, response, phase);
}

/** Calls a typed governed RPC asynchronously with the same validation and metrics. */
export async function rpcAsync<K extends RPCName>(name: K, body: RPCs[NoInfer<K>]['input'], actor: Actor, phase: 'setup' | 'workload' = 'workload'): Promise<RPCs[K]['output']> {
  const request = createRPCRequest(config.baseURL, name, body);
  const response = await http.asyncRequest('POST', request.url, request.body, requestParams(name, actor, phase));
  return rpcResponse(name, response, phase);
}

/** Starts real execution while leaving the VU able to observe and cancel its lease. */
export function executeAsync(requestId: string, actor: Actor): Promise<RPCs['QueryExecutions.Execute']['output']> {
  return rpcAsync('QueryExecutions.Execute', { requestId }, actor);
}

/** Consumes Connect JSON envelopes and requires a successful end-of-stream before returning CSV. */
export function exportCSV(requestId: string, actor: Actor): string {
  const payload = encodeJSON(executionRequest, { requestId });
  const payloadBytes = new Uint8Array(encoding.b64decode(encoding.b64encode(payload)));
  const frame = new Uint8Array(5 + payloadBytes.length);
  new DataView(frame.buffer).setUint32(1, payloadBytes.length);
  frame.set(payloadBytes, 5);
  const response = http.post(`${config.baseURL}/portcullis.v1.QueryExecutions/ExportCSV`, frame.buffer, {
    headers: { 'Content-Type': 'application/connect+json', 'Connect-Protocol-Version': '1',
      Cookie: `__Host-portcullis_session=${actor.session}; __Host-portcullis_csrf=${actor.csrf}`, 'X-CSRF-Token': actor.csrf },
    tags: { name: 'QueryExecutions.ExportCSV', phase: 'workload' }, timeout: '10s', redirects: 0, responseType: 'binary',
  });
  latency.add(response.timings.duration, { rpc: 'QueryExecutions.ExportCSV' });
  samples.add(1, { rpc: 'QueryExecutions.ExportCSV' });
  if (response.status === 429) throttled.add(1);
  if (!check(response, { 'CSV transport succeeds': value => value.status === 200 })) throw new Error(`CSV returned HTTP ${response.status}`);
  const bytes = new Uint8Array(response.body);
  let offset = 0;
  let csv = '';
  let ended = false;
  while (offset < bytes.length) {
    if (ended || offset + 5 > bytes.length) throw new Error('Invalid CSV envelope');
    const flag = new DataView(bytes.buffer).getUint8(offset);
    const length = new DataView(bytes.buffer).getUint32(offset + 1);
    offset += 5;
    if (offset + length > bytes.length) throw new Error('Truncated CSV envelope');
    const text = encoding.b64decode(encoding.b64encode(bytes.slice(offset, offset + length).buffer), 'std', 's');
    if (flag === 2) {
      const message = decodeJSON(z.object({ error: z.unknown().optional() }), text);
      if (message.error !== undefined) throw new Error('CSV stream ended with an error');
      ended = true;
    } else if (flag === 0) {
      const message = decodeJSON(z.strictObject({ data: base64Text.default('') }), text);
      csv += encoding.b64decode(message.data, 'std', 's');
    } else throw new Error('Unsupported CSV envelope');
    offset += length;
  }
  if (!ended) throw new Error('CSV end-of-stream missing');
  return csv;
}
