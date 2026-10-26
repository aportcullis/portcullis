# ADR-0045: Serialize container-backed test package processes

- **Status:** Accepted
- **Date:** 2026-10-04
- **Partially superseded by:** [ADR-0088](0088-isolated-testcontainer-sessions.md) for Go package/session scheduling and [ADR-0095](0095-browser-container-ownership.md) for browser database ownership; concurrent verification groups remain in effect.

## Context

Uncached full-suite execution reproduced Testcontainers v0.43.0 reaper startup failures: separate Go package processes discover the shared Ryuk container while it is still created, and readiness refuses before database SQL runs. A successful retry does not establish a reliable gate.
The library uses a process-local mutex and discovers reapers across test processes; Portcullis cannot coordinate those processes with its fixture mutex.

## Decision

Run the canonical Go test and race targets and the PostgreSQL compatibility package command with `-p 1`. This schedules package processes sequentially without disabling `t.Parallel`, scenario-controlled competing callers, shuffled order, uncached execution or the race detector. Retain required real databases and Testcontainers cleanup.
Refuse fixture startup failures rather than skipping or retrying application SQL. Keep the application concurrency and database-locking contracts unchanged.

### Amendment 2026-10-04: concurrent verify groups

`make verify` builds the SPA, Playwright browser and load bundle once.
It then runs three groups concurrently: static checks, the Go test target (still `-p 1`) and the browser suite.

The browser harness is a separate process tree with its own containers, so it runs with `TESTCONTAINERS_RYUK_DISABLED=true` and never joins the reaper startup race.
Per [ADR-0095](0095-browser-container-ownership.md), deferred cleanup attempts to terminate only the database handles returned to the current invocation, including handles returned with a startup error.
Startup leaves pre-existing containers untouched; PID labels are diagnostic metadata because a local process probe cannot determine whether a harness in another namespace has exited. Abrupt termination can leave containers for operator-reviewed cleanup.
Playwright reserves free loopback ports for each run instead of fixed ports, so concurrent browser runs never collide.
Testcontainers documents disabling Ryuk when the environment performs its own cleanup.

Each group keeps its steps in order and writes `<group>.log` to a fresh per-run temporary directory outside the checkout (`$TMPDIR/portcullis-verify.XXXXXX`, prefix overridable with `VERIFY_LOG_DIR`).
Endpoint protection on the working tree therefore cannot refuse a rewrite of the previous log.
Group commands and their merged standard output/error stream live through `tee` while the same output is retained in the group log. Each group announces its start and log path, then reports its elapsed time and outcome. The group recipe uses Bash `pipefail`, so a failed verification command or log write fails the group. Recursive Make and its existing process-group cancellation remain in place. Concurrent groups may interleave their output on GNU Make 3.81.

### Functional authorization budgets

The real-binary browser harness explicitly uses a one-second condition budget and a five-second resource-authorization checkpoint budget, matching the finite checkpoint budget in the API integration fixture. Concurrent verification can delay an individual metadata snapshot beyond the production default of 250 milliseconds. An observed 350-millisecond snapshot lock wait reproduces that default's deadline refusal; the same grant and wait succeed with the explicit five-second functional budget. This comparison isolates a failure path without establishing which checkpoint expired in a prior browser failure.

Production defaults, the five-second coherent-read bound and refusal after an actual deadline remain unchanged. Functional browser journeys establish correctness under these declared fixture budgets; they do not qualify production latency or capacity. Deadline scenarios retain no result rows, explicit read retry, late-response fencing and zero SQL replay. The execution journey waits for the restored original-order rows before applying its next filter. See [Playwright web-first assertions](https://playwright.dev/docs/test-assertions) and [Connect deadline errors](https://connectrpc.com/docs/protocol/).

## Consequences

Package compilation and execution become less parallel, increasing cold gate time.
The concurrent verify groups recover most of it, bounding the wall time by the browser suite.
This is a bounded harness workaround, not an upstream-library fix or proof that every Docker lifecycle failure is eliminated.
Requalify parallel package scheduling when the pinned library resolves the startup race.
Product requirements remain unchanged.

## Sources

- [Testcontainers for Go configuration](https://golang.testcontainers.org/features/configuration/) (checked 2026-10-04): `TESTCONTAINERS_RYUK_DISABLED` stops automatic cleanup and suits environments that clean up containers themselves.
- [Go test/build flags](https://pkg.go.dev/cmd/go): `-p` limits concurrent programs and test binaries; `-parallel` governs parallel tests inside each binary.
- [Pinned Testcontainers reaper implementation](https://github.com/testcontainers/testcontainers-go/blob/v0.43.0/reaper.go): process-local synchronization and reaper discovery/reuse across test processes.
