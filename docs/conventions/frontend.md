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
    ui/       #   component directories: implementation, public API, example and README (see "Vendored UI" below)
    lib/      #   pure helpers (cn, csrf, formatting)
    api/      #   Connect transport + clients (re-exports from gen/)
  gen/        # buf codegen output — NEVER hand-edited, imported only by shared/api
              # and for generated types
```

### The one dependency rule
Imports point **downward only**: `app → pages → features → entities → shared` (`gen` sits below `shared`).
Same-layer imports across folders are forbidden (a feature never imports another feature; an entity never imports another entity) — shared code moves DOWN a layer instead.
This is the SPA's ports-and-adapters: the same inward-only dependency discipline as the Go layers.

**Oxlint enforces all of it** (`web/oxlint.config.ts`): the `@/` alias, the downward-only layers, and — since 2026-07-26 — the sibling-slice ban, generated per slice from the folders under `features/` and `entities/`.
It also rejects `as` type assertions and `!` non-null assertions in `src/` and `e2e/` (ADR-0031); narrow with a type guard or a parse function at the DOM or JSON boundary instead.
The sibling rule was review-only until a feature did import another one; a convention a tool cannot check is a convention that erodes.
When shared logic needs a caller-specific piece (an error formatter, a label), **inject it** rather than importing sideways — see `shared/lib/openFetch.ts`.

### Slicing
- Split components by **user action**, not by screen: a page composes features; a feature owns one interaction (form, button, dialog) end-to-end.
- A widgets/ layer (multi-feature blocks) is introduced only when a composition is reused across pages — don't pre-create it.

## Imports
- **Always the `@/` alias** from `web/src` (`@/shared/ui/button`).
  Relative import paths — `./` AND `../`, including same-slice sibling files — are **forbidden**: write `@/entities/connection/model`, never `./model`.
  Enforced by Oxlint (`no-restricted-imports`, `make web-lint`).

## Vendored UI (shared/ui)
- Components are copy-pasted from solid-ui (ADR-0013), reviewed on copy-in, and OWNED here: restyle freely, never blindly re-sync with upstream.
  Keep their internal import fixed to `@/shared/lib/utils`.
- Add a component only when a feature needs it; prefer extending an existing one over vendoring a near-duplicate.

Each component family lives in its own directory with an explicit `index.ts`, implementation, `example.tsx` and README. Import from the directory (existing paths are preserved); do not add a root barrel or wildcard exports. Keep examples out of production entry points and implementations independent of their own index to avoid cycles.
See the [UI kit catalog and reuse guide](../../web/src/shared/ui/README.md) for behavior, dependencies, CSS and licensing.

## Server interaction
- All RPC goes through `shared/api` clients (Connect-ES v2, `createClient`); no raw fetch to API routes.
  The CSRF interceptor lives there — features/entities never touch cookies directly.
- Auth/session errors render **uniform messages** (one string for every login rejection — the server is oracle-free and the UI must not undo that, ADR-0006).
- **Fence dialog sessions**
  - Fence work after every `await` through `shared/lib/dialogSession.ts`; closing and reopening remains possible while requests run.
  - Apply nothing when `runInSession` returns `superseded`; the close handler releases `busy`, `saving` and read loading immediately, even if the old request never settles. Old callbacks must not clear a reopened session's loading state.
  - Use `createOpenFetch` for the same protection on read-on-open callbacks.
- **Own drafts at list level**
  - Solid’s `<For>` is [keyed by reference](https://docs.solidjs.com/reference/components/for), so refreshed row objects destroy row-owned forms.
  - Keep the edited row in a list signal and resolve it by ID through `features/connection/editTarget.ts`.
  - Retain the captured row when reload fails and empties the list.
  - Reset through a `createMemo` of the target ID; row-object changes must not discard the session or input.
- **Gate each action independently**
  - Ask for the permission its RPC requires (ADR-0008/0018).
  - Keep Submit/Cancel on the list row under `requests.create`; Details separately needs `requests.get`.
  - Offer saved drafts only when the caller can reopen them (`mayReturnToSavedDraft`).

## Testing
- Browser e2e: Playwright under `web/e2e/`, driving the real Go binary + throwaway PostgreSQL (`make e2e`).
  Serial (`workers: 1`) — bootstrap is once per database.
- Type safety is enforced by `pnpm -C web typecheck` (TypeScript 7 native `tsc`).
- Unit tests: vitest (`make web-test`, part of `make verify`), colocated as `*.test.ts`, node environment (`web/vitest.config.ts` mirrors the `@/` alias).
  Reserved for pure logic Playwright cannot schedule deterministically — store/state race interleavings are the canonical case (`entities/connection/store.test.ts`); component rendering stays with e2e.
- Every scenario has at least three or four success cases and three or four failure cases, and the [naming rules](code.md#naming--the-name-is-the-interface) apply to TypeScript too: no single-letter callback or event parameters (`event`, `parameterType`, not `e`, `t`).

## Page-first workflows

Routine create/edit/detail/review/settings/results workflows use pages with direct URLs and visible navigation (ADR-0022). Reserve modal confirmation for risky or destructive actions. Read-only expansion, full cells and validation errors stay inline. Fence asynchronous reads/mutations on route unmount and session changes; background refresh must preserve active SQL edits and decision reasons.

## Changing the UI

Design values live in [`web/src/app/theme.css`](../../web/src/app/theme.css): light/dark semantic colors, `--ui-font`, `--code-font`, `--radius`, `--content-width`, `--page-padding`, `--section-gap` and `--header-padding`. The defaults define the current application appearance.
Global Tailwind mappings stay in `index.css`; use semantic utilities such as `bg-background`, `text-muted-foreground` and `font-mono` so overrides reach components.

Change application layout in [`layout.css`](../../web/src/app/layout.css) and [`ApplicationFrame`](../../web/src/shared/ui/ApplicationFrame/ApplicationFrame.tsx). The frame accepts brand/navigation/account/content slots and has no session, permission or RPC knowledge. AppShell owns those decisions and supplies the slots.
For a sidebar layout, rearrange the frame rather than duplicating authorization or navigation logic.

Restyle owned primitives in `shared/ui`; assemble features in `pages`. Branding is centralized through `BrandLogo` and `web/public/brand/` assets. UI asset changes should update README media when they alter the documented appearance. This is a source customization guide; a runtime theme picker and organization branding are separate product work (ADR-0036).

For example, edit `--content-width: 80rem` and `--page-padding: 2rem` in `theme.css` to widen the application and add space. Set `--radius: 0.75rem` to adjust shared rounded controls. Edit the light and dark semantic colors together and check text, errors and focus visibility in both. Make every test pass before committing, including `make verify`'s web checks and all browser workflows.

Use the [UX research and evaluation protocol](../design/ux-evidence.md) when changing workflow hierarchy. Loading skeletons use fixed decorative bars; keep paging, error and empty-state contracts independent of loading presentation.
