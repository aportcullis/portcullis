import { fail, sleep } from 'k6';

import { config, needsReview, vus } from './config.ts';
import { rpc } from './client.ts';
import type { Actor, Fixture } from './contracts.ts';

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function actor(value: unknown): value is Actor {
  return record(value) && typeof value.session === 'string' && !!value.session
    && typeof value.csrf === 'string' && !!value.csrf;
}

const input: unknown = JSON.parse(open(config.fixturePath));
if (!Array.isArray(input) || input.length < vus) {
  throw new Error('Provide at least one distinct requester fixture per VU');
}

export const fixtures: Fixture[] = input.slice(0, vus).map((value: unknown) => {
  if (!record(value) || typeof value.connectionId !== 'string' || !value.connectionId || !actor(value.requester)) {
    throw new Error('Fixture needs connectionId and requester session/CSRF');
  }
  if (needsReview && !actor(value.approver)) throw new Error('Review needs an approver fixture');
  return { connectionId: value.connectionId, requester: value.requester,
    approver: actor(value.approver) ? value.approver : undefined };
});

export function verifyFixtures(): void {
  const identities = new Set<string>();
  for (const fixture of fixtures) {
    const requester = rpc('Auth.Me', {}, fixture.requester, 'setup');
    if (identities.has(requester.user.id)) fail('Each VU needs a distinct requester');
    identities.add(requester.user.id);
    if (needsReview && fixture.approver) {
      const approver = rpc('Auth.Me', {}, fixture.approver, 'setup');
      if (approver.user.id === requester.user.id) fail('Self-approval is forbidden');
    }
    sleep(0.5);
  }
}
