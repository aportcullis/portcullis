import { fail, sleep } from 'k6';

import { config, needsReview, vus } from './config.ts';
import { rpc } from './client.ts';
import { fixtureSchema, type Fixture } from './contracts.ts';
import { decodeJSON } from './json.ts';
import { z } from 'zod';

const input = decodeJSON(z.array(fixtureSchema).min(vus), open(config.fixturePath));
export const fixtures: Fixture[] = input.slice(0, vus);
if (needsReview && fixtures.some(fixture => fixture.approver === undefined)) {
  throw new Error('Review needs an approver fixture');
}

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
