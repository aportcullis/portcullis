# Portcullis UI kit

Domain-free SolidJS components owned by this project. Each component family has its implementation, explicit public API, usage notes and a type-checked example together. Existing application imports remain unchanged.

```text
shared/ui/button/
  button.tsx   # implementation and variants
  index.ts     # explicit public exports
  example.tsx  # usage example checked by TypeScript and lint
  README.md    # behavior, accessibility and customization
```

Import from a component directory, for example `import { Button } from "@/shared/ui/button"`. There is no root barrel. Examples are not imported into application routes or production entry points. Internal files must not import their own index, which would introduce a cycle.

## Catalog

| Family | Purpose |
|---|---|
| [button](button/README.md) | Actions, variants and sizes |
| [badge](badge/README.md) | Short status labels |
| [avatar](avatar/README.md) | Stable local default profile images |
| [alert](alert/README.md) | Important inline feedback |
| [card](card/README.md) | Grouped content |
| [dialog](dialog/README.md) | Risky-action confirmation |
| [table](table/README.md) | Semantic table primitives |
| [text-field](text-field/README.md) | Accessible labeled inputs and validation |
| [ApplicationFrame](ApplicationFrame/README.md) | Brand, navigation, account and content slots |
| [PageHeader](PageHeader/README.md) | Page title, purpose and actions |
| [BrandLogo](BrandLogo/README.md) | Light/dark wordmark |
| [InlinePanel](InlinePanel/README.md) | Inline region with initial focus |
| [LoadingSkeleton](LoadingSkeleton/README.md) | Bounded loading status |

## Reuse in this repository

Compose these APIs in features and pages; keep authorization, RPCs, database state and request/execution logic in their existing layers. Edit semantic tokens in [theme.css](../../app/theme.css), layout in [layout.css](../../app/layout.css), and Tailwind mappings/base styles in [index.css](../../app/index.css). Use semantic utilities, preserve light/dark contrast, and supply labels for controls.

The examples are source examples, not a hosted component gallery. TypeScript checks every `example.tsx` through `tsconfig.app.json`, but runtime behavior is checked by existing product browser scenarios. For a new interaction, first add an observable scenario, then implement and verify it.

## Reuse in another SolidJS application

This is source reuse rather than a separately published npm package. Copy the selected component directory and its transitive shared dependencies. Most primitives use [utils.ts](../lib/utils.ts) (`clsx` and `tailwind-merge`); buttons/badges use `class-variance-authority`, and interactive primitives use `@kobalte/core`. InlinePanel also requires the button family. Consult [package.json](../../../package.json) for the exact reviewed dependency versions and installation policy.

Configure the `@/` alias to your source root or deliberately rewrite imports. Configure SolidJS JSX and Tailwind v4, load the theme variables and utility mappings, and include the copied sources in Tailwind detection. ApplicationFrame, PageHeader and LoadingSkeleton also require their named layout rules. These components are not directly compatible with React or Vue. BrandLogo needs the [brand assets](../../../public/brand/) at the documented URLs; review [branding guidance](../../../../docs/branding.md) when reusing project identity.

Portcullis-owned source is [Apache-2.0](../../../../LICENSE); retain [NOTICE](../../../../NOTICE). Vendored solid-ui components and the adapted theme retain their [upstream MIT license and attribution](LICENSE.solid-ui) (see [ADR-0013](../../../../docs/adr/0013-spa-ui-foundation.md)); third-party dependency licenses remain applicable. Include `LICENSE.solid-ui` when redistributing these vendored sources or the adapted theme. Do not assume the project license replaces upstream notices.

## Verification and contribution

Run `pnpm -C web typecheck`, `pnpm -C web lint`, `pnpm -C web test`, `pnpm -C web build`, and the relevant real-binary browser scenarios. Check light/dark mode, keyboard focus and narrow layouts for visual changes. Keep README media current when documented appearance changes. Add components only for a concrete product need, with explicit exports, a working example and usage/accessibility notes.

The organization follows [FSD component-level public APIs](https://feature-sliced.design/docs/reference/public-api) and the existing [frontend conventions](../../../../docs/conventions/frontend.md).
