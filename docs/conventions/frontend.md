# Frontend conventions (web/ — SolidJS SPA)

Stack: SolidJS + Vite + Tailwind v4 + Kobalte(vendored solid-ui) + Connect-ES v2 (ADR-0013).
Package manager is **pnpm only** (`pnpm -C web …`) — never npm/npx.

## Structure — light FSD (Feature-Sliced Design)

```
web/src/
  app/        # composition root: entry wiring, router, providers, global CSS
  pages/      # one component per route; ASSEMBLY ONLY (no business logic, no fetching)
  features/   # user actions, one folder per domain area (auth/, connection/, …);
              # each interactive unit is its own small component (LoginForm,
              # GoogleLoginButton — separate files, composed by pages)
  entities/   # domain state, one folder per entity (session/, request/, …):
              # stores/signals + entity-scoped API orchestration
  shared/     # the base layer, no domain knowledge:
    ui/       #   vendored solid-ui components (see "Vendored UI" below)
    lib/      #   pure helpers (cn, csrf, formatting)
    api/      #   Connect transport + clients (re-exports from gen/)
  gen/        # buf codegen output — NEVER hand-edited, imported only by shared/api
              # and for generated types
```

### The one dependency rule
Imports point **downward only**: `app → pages → features → entities → shared` (`gen` sits below
`shared`). Same-layer imports across folders are forbidden (a feature never imports another
feature; an entity never imports another entity) — shared code moves DOWN a layer instead. This is
the SPA's ports-and-adapters: the same inward-only dependency discipline as the Go layers.

### Slicing
- Split components by **user action**, not by screen: a page composes features; a feature owns one
  interaction (form, button, dialog) end-to-end.
- A widgets/ layer (multi-feature blocks) is introduced only when a composition is reused across
  pages — don't pre-create it.

## Imports
- Always the `@/` alias from `web/src` (`@/shared/ui/button`); **no relative `../` imports across
  folders** (sibling-file `./` imports inside one slice are fine).

## Vendored UI (shared/ui)
- Components are copy-pasted from solid-ui (ADR-0013), reviewed on copy-in, and OWNED here:
  restyle freely, never blindly re-sync with upstream. Keep their internal import fixed to
  `@/shared/lib/utils`.
- Add a component only when a feature needs it; prefer extending an existing one over vendoring a
  near-duplicate.

## Server interaction
- All RPC goes through `shared/api` clients (Connect-ES v2, `createClient`); no raw fetch to API
  routes. The CSRF interceptor lives there — features/entities never touch cookies directly.
- Auth/session errors render **uniform messages** (one string for every login rejection — the
  server is oracle-free and the UI must not undo that, ADR-0006).

## Testing
- Browser e2e: Playwright under `web/e2e/`, driving the real Go binary + throwaway PostgreSQL
  (`make e2e`). Serial (`workers: 1`) — bootstrap is once per database.
- Type safety is enforced by `pnpm -C web typecheck` (tsgo); no separate unit-test runner until a
  pure-logic module warrants one.
