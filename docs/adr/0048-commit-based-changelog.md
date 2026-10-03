# ADR-0048: Generate reviewed changelogs with git-cliff

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

Portcullis releases one container and already records small Conventional Commits. Contributors need readable, traceable release history without independent npm-package versioning or an automated tag owner.

## Options

- git-cliff: configurable local generation from existing commits and tags; leaves release ownership with maintainers.
- Release Please: maintains release PRs and automates changelogs, version changes, tags and GitHub Releases. Useful later if that broader workflow is adopted.
- Changesets: contribution-time release notes and coordinated package versions; its monorepo/package focus adds process for this single distributed application.

## Decision

Use git-cliff 2.14.2 through its official digest-pinned container, tracked by Renovate. Mount only the project read-only, disable container networking and external template commands, and pass no host credentials. Generate an English CHANGELOG from full Git history with version/unreleased sections, grouped changes, breaking-change markers and commit links. Retain unconventional commits in Other instead of silently losing history. Skip merge bookkeeping and changelog-only housekeeping, with breaking commits protected.

`make changelog` atomically replaces the committed snapshot after successful generation; maintainers review it before release tagging. An optional validated `RELEASE_TAG` labels the intended release without creating a Git tag. `make release-notes` selects only the current tag, and the verified GHCR publication job adds those notes to its Actions summary before registry login. Full-history checkout is required. No automatic version bump, tag creation, PR or GitHub Release publication is introduced. Exercise real disposable Git-history scenarios in `make changelog-check`, included in `make verify`.

## Consequences

Docker is required for the pinned generator, as already required for integration gates. Initial generation lists unreleased development history and does not establish feature acceptance. Generated notes need editorial review; they cannot infer product intent from every small internal commit. The checked-in snapshot is refreshed for releases, not after every development commit, so routine CI does not demand a self-referential changelog update.

## Sources

- [git-cliff](https://git-cliff.org/docs/), [official Docker distribution](https://git-cliff.org/docs/docker/), [configuration](https://git-cliff.org/docs/configuration/git/), [CLI](https://git-cliff.org/docs/usage/args/).
- [Release Please](https://github.com/googleapis/release-please).
- [Changesets](https://github.com/changesets/changesets).
