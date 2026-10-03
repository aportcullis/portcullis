# Draft: Explain reversible SQL formatting with a small example

**Publication status:** draft. Maintainer reviewer: assign before publishing. Proposed labels after confirmation: `help wanted`, `good first issue`.

## Problem

The product tour mentions automatic, reversible SQL formatting but does not walk a new user through enabling it, undoing it, formatting manually, and saving a draft.

## Scope and starting points

Expand the request section of the [product tour](../../media/product-tour.md) with one short synthetic SQL example and a clear sequence. Read the formatting scenario in [requests.spec.ts](../../../web/e2e/requests.spec.ts) and [SQL formatting behavior](../../../web/src/shared/lib/sqlFormatting.ts). Explain current behavior; do not introduce editor features or recreate screenshots.

## Completion criteria

- Show auto-format on leaving the editor, Undo formatting, and Format SQL.
- Explain that an invalid statement stays unchanged and that formatting alone does not save or submit a request.
- Use only supported, synthetic SQL and the current control names.
- Check the example against the existing browser scenario or disposable UI; document any unverified observation.
- Local links resolve and `git diff --check` passes. No new automated test is required for prose changes.

Comment before starting; the assigned reviewer helps verify the example. Follow [CONTRIBUTING](../../../CONTRIBUTING.md).
