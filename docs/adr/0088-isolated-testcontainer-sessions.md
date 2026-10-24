# ADR-0088: Isolate Testcontainers sessions between test packages

- **Status:** Accepted
- **Date:** 2026-10-25
- **Partially supersedes:** ADR-0045 for Go package/session scheduling; its concurrent verification groups and browser cleanup remain unchanged.

## Context

A complete gate observed a metadata fixture discovering a Ryuk container already being removed before SQL ran. A fresh unchanged gate passed, but that does not establish lifecycle reliability or identify the deleting actor. Sequential package execution still shares one parent Go process. Pinned Testcontainers v0.43.0 derives its session from that parent's identity and can therefore rediscover the preceding package's reaper. Its versioned implementation does not support the session-override setting described by newer documentation.

## Decision

Expand the requested package patterns once using standard go list in the same build context as the tests, including race selection. Refuse selection errors or an empty result before starting a test. Run each selected package sequentially in its own standard go test invocation so package binaries have independent parent processes and Testcontainers sessions.

Retain uncached shuffled execution, required real databases, within-package parallel tests, competing-caller scenarios and the race detector. Keep package selection available through GO_TEST_PACKAGES, and route the compatibility workflow through the same canonical target. Aggregate ordinary package failures while continuing to test later selected packages, as the existing complete suite does. Standard Make/terminal job-group interruption or termination stops the active work and prevents another package from starting; this does not promise recursive signal forwarding when only the runner PID is signalled. Never turn a selection, compilation, fixture or assertion failure into a successful gate.

Do not disable the Go fixture reaper, retry failed database startup, change production limits or introduce a custom test-binary execution wrapper. Preserve the separate browser harness cleanup and concurrent verification groups. Leave direct developer go test commands available with their standard behavior.

## Consequences

Independent invocations add Go command startup and package-loading work, trading gate time for an explicit ownership boundary. This prevents cross-package reuse of the pinned library's session; it is not an upstream reaper fix or proof that every Docker lifecycle failure is eliminated.

Qualify real Go package selection and process identity, the pinned public Testcontainers session API, repeated uncached execution and race-only build selection in disposable fixtures. Verify refusal before partial work, continued failure reporting and unchanged required-database admission. Then run complete functional/security gates and actual database/browser journeys, review code, architecture, operator behavior and the staged change, and record measured time before the focused commit.

## Sources (verified 2026-10-25)

- [Pinned Testcontainers session derivation](https://github.com/testcontainers/testcontainers-go/blob/v0.43.0/internal/core/bootstrap.go).
- [Pinned Testcontainers reaper discovery](https://github.com/testcontainers/testcontainers-go/blob/v0.43.0/reaper.go).
- [Go package selection, build and test flags](https://pkg.go.dev/cmd/go).
- [Bash foreground job-group signal handling](https://www.gnu.org/s/bash/manual/html_node/Signals.html).
