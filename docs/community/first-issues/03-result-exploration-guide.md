# Draft: Add a concise result-exploration walkthrough

**Publication status:** draft. Maintainer reviewer: assign before publishing. Proposed labels after confirmation: `help wanted`, `good first issue`.

## Problem

Users need a concrete example showing how result sorting, filters, full cells, CSV, and expiry interact. The current tour describes the contracts but has no short step-by-step exercise.

## Scope and starting points

Expand the results section of the [product tour](../../media/product-tour.md) with a small synthetic PostgreSQL result example. Refer to [ResultPanel](../../../web/src/features/request/ResultPanel.tsx), the [result browser scenario](../../../web/e2e/requests-execution.spec.ts), and the [quickstart](../../operations/pg-alpha-quickstart.md). Keep this task to documentation and existing controls.

## Completion criteria

- Demonstrate choosing Sort by and direction, restoring query order, filtering, inspecting a full cell, and preparing/downloading CSV.
- Explain that sorting covers all cached rows without rerunning SQL; CSV uses the complete snapshot in original order despite filtering.
- Explain the 15-minute expiry, possible earlier eviction, and that a truncated snapshot also limits CSV contents.
- Use exact synthetic integer values without assuming JavaScript number precision. Verify example SQL against supported forms with help from the reviewer.
- Local links resolve and `git diff --check` passes. No new automated test is required for prose changes.

Comment before starting; the assigned reviewer helps verify the walkthrough. Follow [CONTRIBUTING](../../../CONTRIBUTING.md).
