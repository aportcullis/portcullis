# ADR-0078: CSS-first stylesheet ownership

- **Status:** Accepted
- **Date:** 2026-10-21
- **Refines:** ADR-0013, ADR-0036 and ADR-0076 for CSS source ownership.

## Context

VS Code and Cursor can report Unknown at rule for valid Tailwind v4 directives when using a standard CSS language mode. The application already uses Tailwind v4 through its dedicated Vite plugin, CSS-first theme mappings, cascade layers and modern native CSS. Application layout currently also contains shared UI and request-specific styles. Editor preferences and an additional language-service dependency are outside this change.

## Decision

Retain the pinned Tailwind v4/Vite integration and valid theme/custom-variant syntax. Do not add editor settings, extension recommendations, a separate CSS language-service dependency or blanket diagnostic suppression. Editor language-mode selection remains a contributor preference; repository compilation does not change editor diagnostics.

Own semantic tokens and stylesheet composition in app. Keep application surface layout in layout.css, shared frame/header/skeleton rules beside their components, and request layout beside the request feature. Compose these styles once through the existing CSS entry and preserve cascade layer, selector, declaration and media-query behavior. Do not add Sass, a legacy Tailwind configuration, runtime style injection, new network requests or speculative browser-specific optimizations. Preserve the existing focus, contrast and protected UI/state contracts from the PRD.

## Verification

This concern relocates existing CSS without a new behavior; do not claim a new behavioral red. Compare selector/declaration/media/layer records before and after relocation, compile through the actual Tailwind/Vite integration, then run make verify and make supply-chain. Keep existing real-binary light/dark, narrow-screen, focus, state-refresh and authority-loss scenarios as regression coverage. This changes source layout, not product requirements or complete WCAG/M1 acceptance.

## Sources (verified 2026-10-21)

- [Tailwind editor setup](https://tailwindcss.com/docs/editor-setup).
- [Tailwind functions and directives](https://tailwindcss.com/docs/functions-and-directives).
- [Tailwind Vite integration](https://tailwindcss.com/docs/installation/using-vite).
