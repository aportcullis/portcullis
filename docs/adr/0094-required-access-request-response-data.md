# ADR-0094: Required access-request response data

- **Status:** Accepted
- **Date:** 2026-10-25
- **Refines:** [ADR-0091](0091-independent-execution-stop-actions.md)'s validated request model and [ADR-0075](0075-local-draft-and-setup-receipt-lifetimes.md)'s creation receipt ownership.

## Context

The Go transport supplies a request after successful Create and UpdateDraft calls and an actor for every recorded approval or rejection. Protobuf message presence still allows an absent field to decode successfully. Returning an unchecked optional request let the composition form report an empty successful receipt, silently skip submission and remain on the page without feedback. Accepting an absent decision actor also allowed incomplete review evidence to render as an unknown person.

## Decision

Reuse the existing access-request schema for the request consumed from Create and UpdateDraft responses, before returning it or refreshing the list. Match a created request's connection ID and an updated draft's request ID to the call's known target before using its receipt. Require an approver for each decision in the shared schema, carry that guarantee through the validated model and render required actor fields. Preserve legitimate empty display names and emails, empty decision lists, exact integer versions and unclassified drafts. Optional authorized payloads and timestamps retain their existing protocol meaning.

The composition form receives a required saved draft on success. Existing SessionOutcome states continue to distinguish successful work, failure and obsolete interaction ownership; the store's generation fence still governs list refresh rather than inventing an absent successful receipt. Keep response validation outside target invalidation for RPC failures so an old malformed success cannot invalidate a replacement principal's target cache.

A malformed successful write response does not prove rollback or supply a usable receipt. Show a sanitized warning that the draft may have been saved and direct the user to check their requests before retrying. Keep local inputs and any already validated draft identity. Do not submit, navigate or replay a mutation automatically after validation fails. Ordinary RPC failures retain their existing classified feedback and recovery behavior.

## Qualification

Observe failures for missing requests, mismatched targets, invalid required fields, missing decision actors and retained unvalidated objects. Qualify normal create/update responses, exact versions and authored text, empty labels and decision lists, malformed data without disclosure, and late malformed delivery after interaction disposal. Browser scenarios cover invalid Create responses for Save and Submit, missing or unrelated UpdateDraft receipts after a saved draft, valid unclassified draft navigation, and safe failed decision reads followed by recovery. Require the complete functional and supply-chain gates, running-application QA and staged review before committing.

## Sources

- [Zod object schemas](https://zod.dev/api#objects), verified 2026-10-25: object properties are required unless explicitly made optional, and parsed objects retain validated fields.
- [Protocol Buffers field presence](https://protobuf.dev/programming-guides/field_presence/), verified 2026-10-25: message presence is distinct from the validity requirements of an application response.
