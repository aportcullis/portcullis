# ADR-0095: Browser container ownership

- **Status:** Accepted
- **Date:** 2026-10-26
- **Partially supersedes:** [ADR-0045](0045-testcontainers-package-scheduling.md)'s browser startup cleanup by owner PID. Go package scheduling, concurrent verification groups, application shutdown and each database's cleanup-context deadline remain unchanged; database stop grace is specified below.

## Context

Native and containerized browser harnesses can share a Docker daemon while observing different process namespaces. The startup sweep enumerated all browser-labelled containers, probed their numeric owner labels with `kill(pid, 0)` in its own namespace and forcibly removed containers it classified as abandoned. A process invisible in that namespace can still be alive elsewhere.

On 2026-10-25, a live native browser harness lost both databases during overlapping Linux verification startup. The resulting database loss caused subsequent connection waits. The daemon-wide sweep could remove another run's resources despite their live owner; a PID label cannot establish ownership across these execution environments.

A partial-startup SIGINT qualification also left an owned database container behind: the default stop wait consumed the ten-second cleanup context before removal could complete.

## Decision

Remove daemon-wide startup container discovery and PID-liveness-based deletion. Retain each non-nil database handle returned to the current invocation, including a handle returned with a startup error, and terminate only those handles through the existing deferred cleanup path. Container labels remain diagnostic metadata.

Keep a ten-second cleanup context for each database and the application's existing shutdown handling. Within that context, request a two-second database stop grace through Testcontainers StopTimeout so removal can use the remaining budget. Docker API work shares the same context; a slow or failed cleanup can still leave a container behind, and an outer shutdown deadline can interrupt cleanup before all handles are terminated.

Startup leaves pre-existing containers untouched. Preserve the existing browser Ryuk configuration, free loopback ports, required databases, test scenarios and complete verification gates. This harness repair changes no product requirement or PRD scope.

## Consequences

With browser Ryuk disabled, SIGKILL, process crashes or Docker unavailability can leave database containers behind. Later runs do not remove those leftovers automatically. An operator must confirm the exact resources and their abandoned ownership before removing them; a shared browser label or an unsuccessful local PID probe is insufficient evidence.

Qualify normal and partial startup cleanup, handled interruption and termination, and preservation of unrelated containers. Simultaneous native and Linux harnesses sharing a daemon must retain their databases until their own cleanup. These scenarios and the complete functional and supply-chain gates require fresh evidence before the focused commit; this decision does not establish a passing run or milestone completion.

## Sources

- [Linux PID namespaces](https://man7.org/linux/man-pages/man7/pid_namespaces.7.html), verified 2026-10-26: PID-based system calls use the caller's namespace, whose process visibility is limited.
- [Testcontainers for Go garbage collection](https://golang.testcontainers.org/features/garbage_collector/), verified 2026-10-26: defer termination of returned container handles; StopTimeout configures stop grace separately from the cleanup context, and disabling Ryuk requires explicit resource cleanup.
