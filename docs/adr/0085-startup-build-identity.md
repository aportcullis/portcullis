# ADR-0085: Startup build identity

- **Status:** Accepted
- **Date:** 2026-10-25
- **Refines:** ADR-0054 for tag-derived startup logging.

## Context

M1 requires operators to identify the serving build from startup logs while keeping Health responses free of version metadata. The source-admission workflow now records the exact release commit and tag identity, but the compiled server has no injected build version. Reading runtime environment or Git history would let deployment state change the reported build independently of its executable.

## Decision

Keep a command-local linker-eligible string variable initialized to the literal development. Untagged builds retain that identity. The release build passes its exact admitted event tag, including the leading v and any prerelease suffix, through a build-stage Docker ARG to Go's -X main.buildVersion linker assignment. Consume the argument in the existing target-platform build while retaining trimpath and stripping flags. Do not derive it from mutable image aliases, git describe, runtime environment or a new configuration setting.

Emit one INFO starting event with the version field immediately after the configured logger is constructed in the server command, before master-key and database initialization. Keep the field on that event rather than attaching it to the shared logger. Later key/DB refusal can therefore identify the attempted build, while invalid logging configuration retains its existing pre-logger refusal. Existing warn/error verbosity filtering suppresses this INFO event normally.

Keep migrate, key rotation, unknown-command behavior, application APIs, Health schemas/bodies/headers and the SPA unchanged. Build identity belongs to startup output only. It does not replace source admission, artifact provenance, a verified image digest or a signature.

## Consequences

The same admitted tag identifies both architectures' release binaries. A stable and prerelease tag on the same source commit can produce different executables and image digests because the embedded identity differs; the commit alias remains a convenience reference rather than artifact identity. Docker build arguments are public metadata and must not contain secrets. Both PRD translations retain their existing version-location requirement.

Build and execute actual development, stable and prerelease test binaries rather than mutating the private variable in-process. Observe a compiling binary's missing-event red before logging implementation, then observe a real image retaining development before adding Docker argument propagation. Cover at least four successful and four refused startup cases, exact JSON/text identity and event ordering, normal log filtering, runtime override refusal and healthy raw Health nonexposure. Retain required configuration/privacy and image platform/non-root/refusal checks. Require both complete gates, actual running-binary/browser QA, code/architecture/design and staged review before the focused commit; record implementation evidence in milestone qualification.

## Sources (verified 2026-10-25)

- [Go linker string assignment](https://pkg.go.dev/cmd/link).
- [Docker build argument scope](https://docs.docker.com/reference/dockerfile/#arg).
- [Docker build variables and public metadata](https://docs.docker.com/build/building/variables/).
