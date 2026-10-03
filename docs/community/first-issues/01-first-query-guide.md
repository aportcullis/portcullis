# Draft: Make the first-query quickstart match the current alpha

**Publication status:** draft. Maintainer reviewer: assign before publishing. Proposed labels after confirmation: `help wanted`, `good first issue`.

## Problem

The [PostgreSQL quickstart](../../operations/pg-alpha-quickstart.md) tells users to submit SQL without filling the required Title field. It also says SQLite is a later milestone, although the [support matrix](../../product/database-support.md) excludes it. This makes the first-user journey misleading.

## Scope and starting points

Update the quickstart only: show a synthetic title and optional explanation before submitting the first query; state MySQL/saved queries as planned and SQLite as excluded. Use the existing [request narrative fields](../../../web/src/features/request/RequestNarrativeFields.tsx) and [request browser scenario](../../../web/e2e/requests.spec.ts) as behavior references. No product code, database scope, or credentials change is needed.

## Completion criteria

- The first-query steps include Title and optional Body using synthetic examples.
- The support statement matches the existing matrix and PRD.
- A maintainer or contributor follows the steps on a fresh disposable Compose installation and records any blockers. If local execution is unavailable, record that and request the reviewer's walkthrough.
- Local links resolve and `git diff --check` passes. No new automated test is required for prose changes.

Comment before starting; the assigned reviewer can help with the local demo. Follow [CONTRIBUTING](../../../CONTRIBUTING.md).
