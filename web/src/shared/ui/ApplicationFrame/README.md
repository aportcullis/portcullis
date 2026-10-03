# ApplicationFrame

Supply brand, navigation, account and children slots. This component owns layout only; the application supplies session, permissions and navigation state. Requires the application-frame rules from layout.css.

## Use

```tsx
import { ApplicationFrame } from "@/shared/ui/ApplicationFrame";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [ApplicationFrame.tsx](ApplicationFrame.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
