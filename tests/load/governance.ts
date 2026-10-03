import { sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate } from 'k6/metrics';

import { config, needsReview, vus } from './config.ts';
import { fixtures, verifyFixtures } from './fixtures.ts';
import { browse, submit, executeAndExplore } from './journeys.ts';

export { options } from './config.ts';
export const setup = verifyFixtures;

const completed = new Counter('journeys_completed');
const failed = new Rate('journey_failed');

export default function (): void {
  if (exec.vu.iterationInScenario === 0 && config.mode !== 'smoke' && config.mode !== 'arrival') {
    sleep((exec.vu.idInTest - 1) * config.thinkSeconds / vus);
  }
  const journeyIndex = (exec.vu.iterationInScenario + exec.vu.idInTest - 1) % 5;
  try {
    const fixture = fixtures[exec.vu.idInTest - 1];
    if (!fixture) throw new Error('No fixture assigned to this VU');
    if (config.journey === 'execute' || (config.journey === 'full' && journeyIndex === 0)) {
      executeAndExplore(fixture);
    } else if (config.journey === 'browse' || ((config.journey === 'mixed' && journeyIndex !== 0) || (config.journey === 'full' && journeyIndex > 1))) {
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
