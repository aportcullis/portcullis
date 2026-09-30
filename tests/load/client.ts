import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

import { config } from './config.ts';
import type { Actor, RPCName, RPCs } from './contracts.ts';

const latency = new Trend('control_plane_ms', true);
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
    timeout: '10s',
    redirects: 0,
  });
  if (phase === 'workload') {
    latency.add(response.timings.duration, { rpc: name });
    if (response.status === 429) throttled.add(1);
  }
  if (!check(response, { 'RPC succeeds': (r) => r.status === 200 })) {
    throw new Error(`${name} returned HTTP ${response.status}`);
  }
  return response.json() as unknown as RPCs[K]['output'];
}
