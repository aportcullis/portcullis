# ADR-0054: Release branches and documentation ownership

- **Status:** Accepted
- **Date:** 2026-10-05

## Context

The first release is not tagged yet, and M1 includes administration and release acceptance criteria.
A passing governance test run must not imply that the expanded milestone is complete.
Product scope, progress, architectural decisions and release versions need distinct sources of truth.

Per [ADR-0048](0048-commit-based-changelog.md), CHANGELOG is generated from reviewed commits and Git tags, with release ownership retained by maintainers.
Per [the milestone index](../milestones/README.md), a milestone defines the scope of the next release; its scope, progress and evidence are separate records.

## Options

- Release directly from main and mix patch maintenance with the next milestone.
- Maintain an independent document or package version alongside Git tags.
- Use a next-release main branch, minor release branches and owner-created Git tags (chosen).

## Decision

### Branches and versions

`main` is always the development line for the next release.
When the current milestone meets its complete acceptance criteria on main, cut `release/vX.Y` from that verified commit; the first branch is `release/v0.1`.
The owner creates and pushes `vX.Y.Z` tags on the matching release branch and maintains that version's patch releases there.
Agents never create release tags or push any ref.

After the release branch is cut, develop the next milestone on `feat/<feature>` branches from main.
The owner reviews and merges them; starting work outside the current milestone requires the owner's approval.
Release-branch patches do not import unrelated next-milestone work.

Only Git tags assign product versions.
PRDs, source files and document titles have no independent revision number; dependency/tool versions and explicit tag examples remain valid evidence.
Before the first tag, the committed CHANGELOG uses `Unreleased`; release headings derive from actual tags rather than a separate document version.

### Verification and commits

Until the owner releases `v0.1.0`, do not use `fix`, `refactor` or `perf` commit types; use the applicable `feat`, `docs`, `test`, `build`, `ci` or other permitted type.
Keep one concern per small commit, pass the entire `make verify` gate before each commit, review the staged diff immediately before committing, and commit before starting the next concern.
Use topic bullets and one-sentence sub-bullets with a final `Verification` topic; do not wrap sentences to a column width or add AI attribution trailers.
`make supply-chain` is also required for milestone completion and release readiness.

### Documentation and ADRs

Each milestone's `scope.md` defines its scope and completion criteria; progress and dated validation results do not change that scope.
Keep execution task lists, review findings and follow-up notes in local untracked working notes; never commit or reference those notes.
PRD §11 contains a summary table and links, while the remaining PRD contains product requirements and acceptance criteria.
Implementation details and decision rationale belong in ADRs; update both PRD translations together when requirements change.

Accepted ADR decision bodies remain unchanged.
A changed decision requires a new ADR stating what it supersedes, why and how the transition works; the older record may gain only a status and replacement link.
Existing amendment history remains as evidence and is not rewritten in bulk.
Narrow factual corrections and prose formatting may be corrected without changing the decision.

Group related sentences into a paragraph and start a new line after the sentence where the topic changes.
Use ordinary development terms and complete sentences; preserve identifiers, constraints and evidence versions.
Cite evidence as `Per <tracked file path or public link>, …` and never name, link or quote ignored or untracked local files in committed documentation, comments or commit messages.

## Consequences

Version ownership is explicit, and patch maintenance stays separate from next-release development.
M1 remains in progress until all scope and verification criteria are met.
These rules establish the release process; workflow implementation is assessed against [M1 scope](../milestones/m1/scope.md).
No branch, tag or push is performed by this documentation change.

The existing [container release guide](../operations/container-releases.md) is the operational release document; a duplicate root release document is unnecessary.
Community policies have their own documents and are outside this release-process decision.

## Primary sources (verified 2026-10-05)

- Per [Prometheus release guidance](https://github.com/prometheus/prometheus/blob/main/RELEASE.md), separate minor release branches isolate release maintenance from main development.
- Per [Kubernetes release guidance](https://kubernetes.io/releases/release/), release stabilization and maintenance use release branches with controlled changes.
- Per [CNCF project templates](https://contribute.cncf.io/projects/best-practices/templates/), contribution, governance, maintainer, security and release documents have distinct purposes.

Portcullis adopts that separation with its own branch names, milestone gates and owner-controlled tagging, without adopting an upstream release cadence.
