import http from 'k6/http';
import encoding from 'k6/encoding';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

import { config } from './config.ts';
import type { Actor, RPCName, RPCs } from './contracts.ts';

const latency = new Trend('control_plane_ms', true);
const executionLatency = new Trend('execution_ms', true);
const targetDuration = new Trend('server_execution_ms', true);
const samples = new Counter('rpc_samples');
const throttled = new Counter('throttled_requests');

export function rpc<K extends RPCName>(name: K, body: RPCs[K]['input'], actor: Actor, phase: 'setup' | 'workload' = 'workload'): RPCs[K]['output'] {
  const [service, method] = name.split('.');
  const response = http.post(`${config.baseURL}/portcullis.v1.${service}/${method}`, JSON.stringify(body), {
    headers: {
      'Content-Type': 'application/json',
      'Connect-Protocol-Version': '1',
      Cookie: `__Host-portcullis_session=${actor.session}; __Host-portcullis_csrf=${actor.csrf}`,
      'X-CSRF-Token': actor.csrf,
    },
    tags: { name, phase },
    timeout: name === 'QueryExecutions.Execute' ? '40s' : '10s',
    redirects: 0,
  });
  if (phase === 'workload') {
    samples.add(1, { rpc: name });
    if (name === 'QueryExecutions.Execute') executionLatency.add(response.timings.duration);
    else latency.add(response.timings.duration, { rpc: name });
    if (response.status === 429) throttled.add(1);
  }
  if (!check(response, { 'RPC succeeds': (r) => r.status === 200 })) {
    throw new Error(`${name} returned HTTP ${response.status}`);
  }
  const result = response.json() as unknown as RPCs[K]['output'];
  if (name === 'QueryExecutions.Execute') targetDuration.add(Number((result as { durationMs?: string }).durationMs || 0));
  return result;
}

/** Consumes Connect JSON envelopes and requires a successful end-of-stream before returning CSV. */
export function exportCSV(requestId: string, actor: Actor): string {
  const payload = JSON.stringify({ requestId });
  const frame = new Uint8Array(5 + payload.length);
  new DataView(frame.buffer).setUint32(1, payload.length);
  for (let index = 0; index < payload.length; index++) frame[5 + index] = payload.charCodeAt(index);
  const response = http.post(`${config.baseURL}/portcullis.v1.QueryExecutions/ExportCSV`, frame.buffer, {
    headers: { 'Content-Type': 'application/connect+json', 'Connect-Protocol-Version': '1',
      Cookie: `__Host-portcullis_session=${actor.session}; __Host-portcullis_csrf=${actor.csrf}`, 'X-CSRF-Token': actor.csrf },
    tags: { name: 'QueryExecutions.ExportCSV', phase: 'workload' }, timeout: '10s', redirects: 0, responseType: 'binary',
  });
  latency.add(response.timings.duration, { rpc: 'QueryExecutions.ExportCSV' });
  samples.add(1, { rpc: 'QueryExecutions.ExportCSV' });
  if (response.status === 429) throttled.add(1);
  if (!check(response, { 'CSV transport succeeds': value => value.status === 200 })) throw new Error(`CSV returned HTTP ${response.status}`);
  const bytes = new Uint8Array(response.body as ArrayBuffer);
  let offset = 0;
  let csv = '';
  let ended = false;
  while (offset < bytes.length) {
    if (ended || offset + 5 > bytes.length) throw new Error('Invalid CSV envelope');
    const flag = bytes[offset];
    const length = new DataView(bytes.buffer).getUint32(offset + 1);
    offset += 5;
    if (offset + length > bytes.length) throw new Error('Truncated CSV envelope');
    let text = '';
    for (let index = offset; index < offset + length; index++) text += String.fromCharCode(bytes[index]!);
    const message = JSON.parse(text) as { data?: string; error?: unknown };
    if (flag === 2) { if (message.error) throw new Error('CSV stream ended with an error'); ended = true; }
    else if (flag === 0 && typeof message.data === 'string') csv += encoding.b64decode(message.data, 'std', 's');
    else throw new Error('Unsupported CSV envelope');
    offset += length;
  }
  if (!ended) throw new Error('CSV end-of-stream missing');
  return csv;
}
