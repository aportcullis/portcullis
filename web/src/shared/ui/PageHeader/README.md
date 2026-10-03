# PageHeader

Supply title and description, with optional eyebrow and actions slots. Renders an h1; normally use one per page. Requires the page-heading rules from layout.css. Permission checks belong in the caller.

## Use

```tsx
import { PageHeader } from "@/shared/ui/PageHeader";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [PageHeader.tsx](PageHeader.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
