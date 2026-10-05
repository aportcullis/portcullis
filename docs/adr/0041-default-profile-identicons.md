# ADR-0041: Default profile identicons

- **Status:** Superseded by [ADR-0056](0056-account-menu-and-csv-dialog.md)
- **Date:** 2026-10-03

## Context

The owner requests visible profile images with GitHub-like generated defaults. The current authenticated-user API has an opaque user ID and display name but no stored image. A new random image on each render would make users harder to recognize; third-party avatar lookups would disclose account identifiers and create a network dependency.

## Decision

Render a local five-by-five mirrored geometric SVG avatar beside the authenticated user's name. Derive its pattern and color deterministically from the opaque user ID, using a small non-cryptographic hash and seeded generator. Never use email, send the seed to an external service, or insert seed text into SVG markup. This is a visual cue, not a unique identifier or authentication proof. Keep the name visible and give the SVG an accessible image label.

Provide the renderer, generator, tests, public exports, usage example and documentation in the shared UI avatar directory. Do not add storage, random registration-time state, upload handling or provider image fetching in this change; those need their own persistence and privacy decisions. Amend both PRD translations for the default account image requirement.

## Consequences

The same account keeps its default across reloads and different browser sessions; changing the display name does not change it. Different IDs usually produce different patterns, but collisions remain possible. Future stored images can reuse this fallback. Verify stable appearance, varied identities, symmetry and browser login/reload/logout behavior.

## Source

- [GitHub's identicons](https://github.blog/news-insights/company-news/identicons/): generated mirrored five-by-five defaults.
