# InlinePanel

Use an inline region for routine composition with open, label and onClose props. Opening scrolls into view and focuses the first enabled field, or the section when no field exists. Dismiss calls the supplied callback. Closing, by Dismiss or by the caller after a save, returns focus to the element that was focused when the panel opened, provided the panel still held focus and that element is still in the document. It does not trap focus; callers own the open state.

## Use

```tsx
import { InlinePanel } from "@/shared/ui/InlinePanel";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [InlinePanel.tsx](InlinePanel.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
