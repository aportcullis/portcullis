# button

Buttons expose default, destructive, outline, secondary, ghost and link variants; sizes are default, sm, lg and icon. Give icon-only controls an accessible name. Use destructive styling only for risky actions. Specify type="button" for non-submit actions inside forms.

Heights share the control theme tokens: sm 36px, default 40px, lg 44px and icon 40px square. Keyboard focus uses the application base outline; destructive fill/foreground are separate from error text.

## Use

```tsx
import { Button } from "@/shared/ui/button";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [button.tsx](button.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
