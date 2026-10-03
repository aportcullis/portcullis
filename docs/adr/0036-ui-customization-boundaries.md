# ADR-0036: Separate UI design values and application layout from governance

- Status: Accepted
- Date: 2026-10-03

## Context

The owner requested freedom to change the UI. The existing FSD layers and owned Kobalte components already isolate interaction logic, but design values share the global CSS entry and application layout is embedded in the session/permission shell.

## Decision

Provide a developer customization foundation using the current SolidJS/Tailwind stack. Keep project-owned light/dark colors, typography, radius, application width and spacing in `web/src/app/theme.css`. Keep layout rules in `layout.css` and Tailwind utility mappings/base rules in `index.css`. Use `@theme inline` for utilities referring to overridable CSS variables, following Tailwind guidance.

Extract a domain-free `ApplicationFrame` into shared UI with brand, navigation, account and content slots. AppShell retains authentication, permissions, pending counts and navigation selection. Keep owned UI primitives, BrandLogo and page assembly as the component/branding customization boundaries. Preserve current layout and workflow defaults.

## Consequences

Developers can restyle the product or replace its frame without moving domain behavior into presentational components. This does not introduce a runtime theme selector, tenant branding storage, arbitrary uploaded CSS/JavaScript or a plugin API. Those need separate requirements if selected. Verify the production CSS build, TypeScript, lint, existing state tests and real browser workflows; this presentation refactor has no new domain scenario.

## Primary sources (verified 2026-10-03)

- [Tailwind theme variables](https://tailwindcss.com/docs/theme), including `@theme inline` for references to other variables.
- [SolidJS props](https://docs.solidjs.com/concepts/components/props), for reactive slot composition without copying/destructuring props.
