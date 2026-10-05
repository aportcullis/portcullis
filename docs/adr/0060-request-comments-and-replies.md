# ADR-0060: Request comments and replies in M3

- **Status:** Accepted
- **Date:** 2026-10-05

## Context

The owner requested comments and replies alongside governed requests. The PRD previously placed all discussion after MVP.
Per [GitHub review documentation](https://docs.github.com/en/pull-requests/how-tos/review-pull-requests/commenting-on-a-pull-request), comments and replies support discussion separately from submitting a review. Portcullis adopts that separation without treating discussion as approval.

## Decision

Include basic request comments and replies in M3. Keep append-only discussion attached to one request, with author, timestamp and a reply link to a comment in that same request and organization. Render text safely and bound input and list retrieval.
Recheck request visibility and explicit commenting permission on every read/write. Derive authorship from the authenticated session; refuse forged authors, cross-request reply references, disabled accounts and revoked access. Commenting never changes submitted SQL, approval state, quorum or execution authority.
Audit comment creation without putting comment bodies into application logs or audit metadata. Keep discussion records behind the same request visibility boundary. Structured SQL review suggestions remain post-MVP scope.
Keep personal pending-review counts separate from discussion unread counts. The current header's pending-list count does not establish either a personal inbox or unread comments.

## Acceptance

Verify comments and replies, authorized reads, safe text rendering and bounded retrieval; reject cross-organization access, forged authorship, invalid reply references and revoked permissions. Confirm no approval, SQL or execution mutation.
Per [M3 scope](../milestones/m3/scope.md), this is planned MVP scope; accepting the decision does not establish delivered comment UI.
