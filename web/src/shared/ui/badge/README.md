# badge

Use default, secondary, destructive or outline for short status labels. Include meaningful text; color alone must not convey status. Badges do not own actions.

## Use

```tsx
import { Badge } from "@/shared/ui/badge";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [badge.tsx](badge.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
