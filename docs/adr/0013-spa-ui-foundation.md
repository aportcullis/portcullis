# ADR-0013: SPA UI foundation — solid-ui components, Tailwind v4, client routing, e2e

- **Status:** Accepted
- **Date:** 2026-07-11

## Context
The M0 auth vertical needs its screen half: a login page (password + the Google backend link), the first-run bootstrap form, and browser-level end-to-end tests (PRD §5.1, todo M0). The PRD pins the UI stack as **Kobalte + Tailwind + TanStack Table** (PRD §5, "데이터 그리드가 제품 핵심") but does not say whether components are hand-styled from Kobalte primitives or adopted from a component collection. `web/` is a bare Vite + SolidJS scaffold with generated Connect-ES v2 stubs and no UI, routing, or test runner.

Ecosystem verified 2026-07-11 (GitHub activity, Docker-independent):
- **shadcn-style Solid ports are built ON Kobalte + Tailwind** — adopting one keeps the PRD stack. Candidates: `stefan-karger/solid-ui` (1.5k★, Tailwind v4 + shadcn registry since 2025-05, ports shadcn/ui and tremor charts, includes a TanStack Table data-table pattern; last push 2026-01) and `hngngn/shadcn-solid` (751★, Kobalte-only, last push 2026-02). Both are **copy-paste collections** (not npm dependencies): the component source is vendored into this repo and owned here, so upstream staleness does not gate us. Kobalte itself is actively maintained (push 2026-07-11).
- Tailwind v4 is current (`@tailwindcss/vite` plugin, CSS-first config via `@import "tailwindcss"`).
- `@solidjs/router` is the standard SolidJS router.

## Decision
- **Components: adopt solid-ui** (copy-paste, vendored under `web/src/components/ui/`). It has the largest component set on exactly the PRD stack, plus the TanStack Table data-table and tremor chart ports we will want for M1/M7. Vendored code is OURS: reviewed on copy-in, restyled freely, never blindly re-synced. shadcn-solid remains a source to crib single components from if useful.
- **Tailwind v4** via `@tailwindcss/vite`; theme tokens as CSS variables in `src/index.css` (solid-ui's convention), dark mode by media query for now.
- **Routing: `@solidjs/router`** from day one — M1 adds connections/requests/approvals screens, so a hand-rolled view switch would be immediately rewritten.
- **Pre-session config: `Auth.GetConfig`** public RPC (`google_enabled`, `needs_bootstrap`) so the SPA can hide the Google button on servers without OIDC and route a fresh install straight to the first-run form. Bootstrap state is install-level, not per-account — no enumeration oracle. The RPC shares the public rate-limit bucket (ADR-0010) and needs no CSRF (read-only, no session).
- **No Google JS SDK** (ADR-0007): the Google button is a plain anchor to `/auth/google/start`.
- **Uniform error rendering**: every `Unauthenticated` from Login shows one string — wrong password, unknown email, disabled account, and progressive-backoff lockout (ADR-0006) are indistinguishable in the UI, matching the server's oracle-free contract.
- **e2e: Playwright** (chromium, `workers: 1`, serial), driving the REAL binary: a `webServer` script boots a throwaway Dockerized PostgreSQL plus `go run ./cmd/portcullis` serving the embedded SPA — fresh DB each run, so the bootstrap flow is always exercised. `__Host-` cookies work over plain HTTP on localhost (browsers treat localhost as a trustworthy origin).

## Consequences
- `web/` gains pinned deps (`@solidjs/router`, `@kobalte/core`, Tailwind v4, cva/clsx/tailwind-merge, `@playwright/test`) — all exact versions, Renovate-managed.
- Vendored solid-ui components carry their MIT license header where present; the collection's license permits this use.
- PRD §5 stays accurate (Kobalte + Tailwind); this ADR only fixes HOW components are produced.
- The e2e suite needs Docker locally and in CI (same constraint as `make test`).
