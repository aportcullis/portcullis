# popover

Anchored Kobalte content rendered directly after its trigger, without a Portal. Use it for a short nonmodal disclosure with ordinary document controls. The caller owns open state, labels, action lifetimes and native focus-exit behavior. Kobalte owns outside-pointer dismissal, Escape and ordinary close focus.

## Use

```tsx
import { Popover, PopoverTrigger, PopoverContent } from "@/shared/ui/popover";
```

Supply an accessible trigger name and a named content region. For disclosure semantics, pass `modal={false}`, `aria-haspopup={false}` on the trigger, and `role="region"` with an accessible label on content. Preserve the trigger's expanded/controls attributes and forward the primitive's pointer handlers. Do not assign menu roles to ordinary buttons.

See the [type-checked example](example.tsx) and the [public API](index.ts). Account-specific Tab exit and asynchronous state belong to the consuming feature. No account, session or transport dependency belongs in this family.

## Customize and reuse

Edit [popover.tsx](popover.tsx) for geometry and semantic colors. Keep content inside the viewport and retain visible focus. Avoid adding focus timers, focusable-element enumeration or a second outside-pointer listener.

This thin wrapper is Portcullis-owned Apache-2.0 source; Kobalte remains its existing dependency. See the [UI kit guide](../README.md) for styling, licenses and verification.
