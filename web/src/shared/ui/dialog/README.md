# dialog

Use only for risky or destructive confirmation; routine forms and details belong in pages or InlinePanel. Include DialogTitle and DialogDescription. Kobalte owns focus trapping, Escape and focus restoration. The content includes a Close control. The example has no destructive side effect.

## Use

```tsx
import { Dialog, DialogTrigger, DialogContent, DialogHeader, DialogFooter, DialogTitle, DialogDescription } from "@/shared/ui/dialog";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [dialog.tsx](dialog.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
