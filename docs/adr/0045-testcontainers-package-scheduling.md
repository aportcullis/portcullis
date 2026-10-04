# ADR-0045: Serialize container-backed test package processes

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

Uncached full-suite execution reproduced Testcontainers v0.43.0 reaper startup failures: separate Go package processes discover the shared Ryuk container while it is still created, and readiness refuses before database SQL runs. A successful retry does not establish a reliable gate. The library uses a process-local mutex and discovers reapers across test processes; Portcullis cannot coordinate those processes with its fixture mutex.

## Decision

Run the canonical Go test and race targets and the PostgreSQL compatibility package command with `-p 1`. This schedules package processes sequentially without disabling `t.Parallel`, scenario-controlled competing callers, shuffled order, uncached execution or the race detector. Retain required real databases and Testcontainers cleanup. Refuse fixture startup failures rather than skipping or retrying application SQL. Keep the application concurrency and database-locking contracts unchanged.

### Amendment 2026-10-04: concurrent verify groups

`make verify` builds the SPA, Playwright browser and load bundle once, then runs three groups concurrently: static checks, the Go test target (still `-p 1`) and the browser suite. The browser harness is a separate process tree that starts its own containers, so it runs with `TESTCONTAINERS_RYUK_DISABLED=true` and never joins the reaper startup race. Its cleanup does not depend on Ryuk: the runner terminates both containers on exit or signal, and `make e2e-run` removes any container labelled `portcullis.test=browser` before and after the suite, covering a killed runner. Testcontainers documents disabling Ryuk when the environment performs its own cleanup. Each group keeps its steps in order and writes its output to `.test-docker/verify/<group>.log`; only a failed group's log is printed, because GNU Make 3.81 on macOS cannot synchronize concurrent output.

## Consequences

Package compilation and execution become less parallel, increasing cold gate time; the concurrent verify groups recover most of it, bounding the wall time by the browser suite. This is a bounded harness workaround, not an upstream-library fix or proof that every Docker lifecycle failure is eliminated. Requalify parallel package scheduling when the pinned library resolves the startup race. Product requirements remain unchanged.

## Sources

- [Testcontainers for Go configuration](https://golang.testcontainers.org/features/configuration/) (checked 2026-10-04): `TESTCONTAINERS_RYUK_DISABLED` stops automatic cleanup and suits environments that clean up containers themselves.
- [Go test/build flags](https://pkg.go.dev/cmd/go): `-p` limits concurrent programs and test binaries; `-parallel` governs parallel tests inside each binary.
- [Pinned Testcontainers reaper implementation](https://github.com/testcontainers/testcontainers-go/blob/v0.43.0/reaper.go): process-local synchronization and reaper discovery/reuse across test processes.
