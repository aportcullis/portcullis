import { z } from 'zod';
import { fixtureSchema, type Fixture } from '#load/contracts';

const reviewFixtureSchema = fixtureSchema.required({ approver: true });

/** Selects the synthetic identity assigned to a k6 virtual user. */
export function fixtureForVirtualUser(assignments: ReadonlyMap<number, Fixture>, virtualUserID: number): Fixture {
  const fixture = assignments.get(virtualUserID);
  if (fixture === undefined) throw new Error('No fixture assigned to this virtual user');
  return fixture;
}

/** Requires a distinct reviewer fixture before starting a review scenario. */
export function reviewFixtureForVirtualUser(assignments: ReadonlyMap<number, Fixture>, virtualUserID: number): z.output<typeof reviewFixtureSchema> {
  const parsed = reviewFixtureSchema.safeParse(fixtureForVirtualUser(assignments, virtualUserID));
  if (!parsed.success) throw new Error('Review needs an approver fixture');
  return parsed.data;
}
