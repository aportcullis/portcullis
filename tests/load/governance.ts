import { sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate } from 'k6/metrics';

import { config, needsReview } from './config.ts';
import { fixtures, verifyFixtures } from './fixtures.ts';
import { browse, submit } from './journeys.ts';

export { options } from './config.ts';
export const setup = verifyFixtures;

const completed = new Counter('journeys_completed');
const failed = new Rate('journey_failed');

export default function (): void {
  try {
    const fixture = fixtures[exec.vu.idInTest - 1];
    if (!fixture) throw new Error('No fixture assigned to this VU');
    if (config.journey === 'browse' || (config.journey === 'mixed' && exec.vu.iterationInScenario % 5 !== 0)) {
      browse(fixture);
    } else {
      submit(fixture, needsReview);
    }
    completed.add(1);
    failed.add(false);
  } catch (error: unknown) {
    failed.add(true);
    console.error(error instanceof Error ? error.message : 'Journey failed');
  }
  if (config.mode !== 'arrival' && config.mode !== 'smoke') sleep(config.thinkSeconds);
}
