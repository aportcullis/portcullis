# alert

Render important inline feedback with a title and explanation. The Kobalte root provides alert semantics; reserve it for actionable feedback rather than ordinary prose. Variants are default and destructive.

## Use

```tsx
import { Alert, AlertTitle, AlertDescription } from "@/shared/ui/alert";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [alert.tsx](alert.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
