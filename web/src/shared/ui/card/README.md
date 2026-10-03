# card

Compose a visually grouped section from the exported parts. CardTitle is an h3; match the surrounding heading hierarchy when using it. Cards do not fetch data or own actions.

## Use

```tsx
import { Card, CardHeader, CardFooter, CardTitle, CardDescription, CardContent } from "@/shared/ui/card";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [card.tsx](card.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
