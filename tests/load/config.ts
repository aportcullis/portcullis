import { z } from 'zod';
import type { Options, Scenario } from 'k6/options';
import type { RPCName } from '#load/contracts';

function choice<T extends string>(name: string, fallback: T, values: readonly T[]): T {
  const value = __ENV[name] || fallback;
  const parsed = z.enum(values).safeParse(value);
  if (!parsed.success) throw new Error(`Unknown ${name}`);
  return parsed.data;
}

function number(name: string, fallback: number, integer = true): number {
  const value = Number(__ENV[name] || fallback);
  if (!Number.isFinite(value) || value < (integer ? 1 : 0) || (integer && !Number.isInteger(value))) {
    throw new Error(`Invalid ${name}`);
  }
  return value;
}

export const config = {
  baseURL: (__ENV.BASE_URL || 'http://localhost:8080').replace(/\/$/, ''),
  mode: choice('MODE', 'smoke', ['smoke', 'load', 'soak', 'stress', 'arrival']),
  journey: choice('JOURNEY', 'browse', ['browse', 'submit', 'review', 'execute', 'mixed', 'full']),
  fixturePath: __ENV.LOAD_FIXTURES || './fixtures.local.json',
  thinkSeconds: number('THINK_SECONDS', 45, false),
};

export const vus = number('VUS', config.mode === 'smoke' ? 1 : 50);
export const needsReview = config.journey === 'review' || ['mixed', 'execute', 'full'].includes(config.journey);
const profiles: Record<typeof config.mode, Scenario> = {
  smoke: { executor: 'per-vu-iterations', vus, iterations: 3, maxDuration: '1m' },
  load: { executor: 'constant-vus', vus, duration: __ENV.DURATION || '10m' },
  soak: { executor: 'constant-vus', vus, duration: __ENV.DURATION || '1h' },
  stress: { executor: 'ramping-vus', startVUs: 0, stages: [
    { duration: '2m', target: Math.max(1, Math.floor(vus / 4)) },
    { duration: '3m', target: Math.max(1, Math.floor(vus / 2)) },
    { duration: '5m', target: vus },
    { duration: '2m', target: 0 },
  ] },
  arrival: { executor: 'constant-arrival-rate', rate: config.mode === 'arrival' ? number('RATE', 1) : 1,
    timeUnit: '1s', duration: __ENV.DURATION || '10m', preAllocatedVUs: vus, maxVUs: vus },
};

let rpcThresholdsForCSV = false;
const measuredRPCs: RPCName[] = [];
if (config.journey === 'browse' || ['mixed', 'full'].includes(config.journey)) {
  measuredRPCs.push('Auth.Me', 'AccessRequests.List', 'AccessRequests.ListRequestableConnections');
}
if (config.journey !== 'browse') {
  measuredRPCs.push('AccessRequests.Create', 'AccessRequests.Submit', 'AccessRequests.Get', 'AccessRequests.Cancel');
}
if (needsReview) measuredRPCs.push('AccessRequests.Approve');
if (config.journey === 'execute' || config.journey === 'full') {
  measuredRPCs.push('QueryExecutions.Get', 'QueryExecutions.GetResult');
  rpcThresholdsForCSV = true;
}
const rpcThresholds: Record<string, string[]> = {};
for (const name of measuredRPCs) {
  rpcThresholds[`control_plane_ms{rpc:${name}}`] = ['p(95)<500'];
  rpcThresholds[`rpc_samples{rpc:${name}}`] = ['count>0'];
}
if (rpcThresholdsForCSV) {
  if (config.journey === 'execute') {
    delete rpcThresholds['control_plane_ms{rpc:AccessRequests.Cancel}'];
    delete rpcThresholds['rpc_samples{rpc:AccessRequests.Cancel}'];
  }
  rpcThresholds['control_plane_ms{rpc:QueryExecutions.ExportCSV}'] = ['p(95)<500'];
  rpcThresholds['rpc_samples{rpc:QueryExecutions.ExportCSV}'] = ['count>0'];
  rpcThresholds['rpc_samples{rpc:QueryExecutions.Execute}'] = ['count>0'];
}

export const options: Options = {
  setupTimeout: '5m',
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(95)', 'p(99)'],
  scenarios: { governance: { ...profiles[config.mode], gracefulStop: '30s' } },
  thresholds: {
    control_plane_ms: ['p(95)<500'],
    http_req_failed: ['rate<0.01'],
    journey_failed: ['rate==0'],
    journeys_completed: ['count>0'],
    checks: ['rate==1'],
    ...rpcThresholds,
    ...(config.mode === 'arrival' ? { dropped_iterations: ['count==0'] } : {}),
  },
  systemTags: ['status', 'method', 'name', 'scenario', 'expected_response'],
};
