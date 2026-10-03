# ADR-0040: Recorded execution time in request and result views

- Status: Accepted
- Date: 2026-10-03

## Context

Execution metadata already persists `duration_ms`; results show an unlabeled millisecond value. The owner wants elapsed query time to be clearly visible.

## Decision

Show a labeled Execution summary with recorded Execution time and rows affected on result pages. Fetch the same durable metadata on terminal request details only for the original requester with `requests.get`; do not expand server authorization or fetch metadata for every list row. Keep read errors/loading inline and fence route/session changes using the existing read helper.

Use the existing server interval: after acquiring the execution lease, before target work, through DB connection/execution, result consumption and snapshot storage, ending before durable completion recording. It excludes approval waiting, browser rendering and later paging/sorting. Label this scope near the metric rather than claiming database-engine-only query time. Render milliseconds below one second and exact three-decimal seconds above it, without converting int64 to JavaScript numbers. A recorded zero for a completed known outcome means less than 1 ms; absent/zero uncertain-outcome metadata is unavailable rather than a successful instant execution. Running states do not present a final duration.

Test execution → recorded summary → reload → same value in results against the real binary, plus presentation scenarios for subsecond/second boundaries and uncertain outcomes. Refresh README application captures after changing their documented appearance.

Sources verified 2026-10-03: [Go monotonic elapsed time](https://pkg.go.dev/time#Since) and [HTML description lists](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/dl). No backend timer or API schema changes are required.
