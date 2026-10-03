import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fixtureForVirtualUser, reviewFixtureForVirtualUser } from '#load/fixture-assignment';
import type { Fixture } from '#load/contracts';

const requesterFixture: Fixture = { connectionId: 'browse-target', requester: { session: 'requester', csrf: 'requester-csrf' } };
const reviewFixture: Fixture = { connectionId: 'review-target', requester: { session: 'review-requester', csrf: 'review-csrf' }, approver: { session: 'approver', csrf: 'approver-csrf' } };
const assignments = new Map<number, Fixture>([[7, requesterFixture], [2, reviewFixture]]);

void test('each virtual user gets its assigned identity regardless of collection order', () => {
  assert.equal(fixtureForVirtualUser(assignments, 2), reviewFixture);
  assert.equal(fixtureForVirtualUser(assignments, 7), requesterFixture);
});

void test('an unassigned virtual user cannot borrow another requester identity', () => {
  assert.throws(() => fixtureForVirtualUser(assignments, 1), /No fixture assigned/);
  assert.throws(() => fixtureForVirtualUser(assignments, 0), /No fixture assigned/);
});

void test('a review scenario requires an approver before it can use its fixture', () => {
  assert.deepEqual(reviewFixtureForVirtualUser(assignments, 2), reviewFixture);
  assert.throws(() => reviewFixtureForVirtualUser(assignments, 7));
});
