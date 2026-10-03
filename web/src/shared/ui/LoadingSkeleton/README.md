# LoadingSkeleton

Supply a meaningful loading label. The status region is busy and the three decorative bars are hidden from assistive technology. Size stays bounded regardless of result size. Requires loading-skeleton and content-surface styles from layout.css; callers handle empty, error and loaded states separately.

## Use

```tsx
import { LoadingSkeleton } from "@/shared/ui/LoadingSkeleton";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [LoadingSkeleton.tsx](LoadingSkeleton.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
