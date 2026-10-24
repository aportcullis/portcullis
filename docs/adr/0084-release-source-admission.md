# ADR-0084: Release source admission

- **Status:** Accepted
- **Date:** 2026-10-24
- **Refines:** ADR-0047 and ADR-0054 for release-branch admission and immutable verification source.

## Context

Release admission currently checks only tag-name syntax. Later jobs check out the event reference without recording the source admitted by the branch policy. Git stores object references and reachability, not the branch on which someone originally created a tag. Matching the current release-branch tip is an explicit admission policy, stronger than accepting any ancestor.

## Decision

Keep the existing standalone SemVer-name validator usable before a tag exists, including changelog previews. Add a source-admission operation taking the validated release tag and the event's full commit object ID. Derive release/vMAJOR.MINOR only from the admitted tag, refresh that exact origin branch and tag into private refs atomically, and refuse missing refs, non-commit targets, failed refreshes or mismatches. The peeled tag commit, matching release-branch tip, event commit and checked-out HEAD must agree at admission. Do not fall back to stale local refs or infer a creation branch from ancestry.

Emit source_sha, the raw tag_oid and release_branch only after every check succeeds, with fixed literal output keys and validated values. Keep diagnostics on stderr and emit no success output on refusal. Support lightweight and annotated tags, recording the raw annotated object as well as its peeled commit so reannotation onto the same commit cannot silently replace the admitted tag.

Check out the event SHA for admission. Pass the admitted source SHA explicitly into reusable CI and every functional, race, frontend, native-image and publication checkout. Validate the reusable input once in an always-running preflight job before its four dependent jobs: only empty or a full 40/64-character hexadecimal object ID is allowed. An empty value preserves the pinned checkout action's existing event ref/commit defaults for ordinary push and pull-request runs. Keep the local reusable workflow tied to its caller's commit and pass data through typed workflow inputs and environment values rather than interpolating release data into shell source.

Immediately before the publication step, refresh the exact tag again and require the recorded raw tag object, peeled commit and checked-out source to remain equal. A deleted or moved tag and same-commit reannotation refuse publication. The release branch may advance during this already-admitted run; do not require its later tip to remain frozen. A new whole-workflow admission after the branch advances intentionally refuses an older tag. Document this rerun tradeoff in both PRD translations and the release procedure.

Keep release-tag creation, updates and deletion owner-controlled through repository rules and the existing owner procedure. A Git comparison cannot prove who created a ref or lock a remote ref after the final check. Do not substitute an actor-equals-repository-owner check, which would be incorrect for organization repositories. Agents do not create product tags or push refs.

## Consequences

Verification and publication use one admitted source even when branch refs progress. Admission is strict and fails closed on unavailable Git evidence. Version injection and promotion of the verified image digest remain separate M1 concerns; this source change does not establish them or prove hosted registry permissions and publication.

Use isolated disposable Git repositories for at least four successful and four refused scenarios. Observe behavioral red against the compiling name-only admission before implementation. Qualify branch-tip mismatch, event/checkout mismatch, annotated and lightweight refs, missing/deleted refs, same-commit reannotation, remote failure and branch advancement during the final fence. Keep the original checkout unchanged, then require workflow lint, both full repository gates, code/architecture/design and staged review, and actual running-application regression QA before the focused commit.

## Sources (verified 2026-10-24)

- [Git typed object dereferencing](https://git-scm.com/docs/git-rev-parse).
- [Git explicit refspecs and atomic fetch](https://git-scm.com/docs/git-fetch).
- [GitHub push event commit](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#push).
- [GitHub reusable workflow inputs and caller commit](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows).
- [Checkout ref and SHA selection](https://github.com/actions/checkout).
