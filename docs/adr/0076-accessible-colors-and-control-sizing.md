# ADR-0076: Accessible semantic colors and shared control sizing

- **Status:** Accepted
- **Date:** 2026-10-21
- **Refines:** ADR-0013 and ADR-0036–0037 for shared presentation only; ADR-0071 and ADR-0075 state lifetimes remain binding.

## Context

The reviewed Portcullis design proposal distinguishes current code from proposed palettes, sizes and screen previews. Current dark destructive colors have insufficient text contrast, input-invalid utilities reference undefined tokens, and native controls duplicate size and focus rules. Treat the proposal as design input, not runtime component or accessibility evidence.

## Decision

Keep the restrained H190 teal identity in both themes and align low-chroma neutral surfaces around H220. Define only semantic colors currently consumed. Separate destructive action fill/foreground and its opaque hover fill from readable error text and error surfaces. Decorative borders may remain subtle; input boundaries and keyboard focus must remain identifiable. Require at least 4.5:1 for ordinary enabled text and 3:1 for necessary control boundaries and focus indicators against adjacent surfaces, including hover states. Preserve explicit error text, labels, native validation and disabled/pending semantics; color never supplies the only status cue. Disabled controls remain visibly disabled and are outside these enabled-contrast thresholds.

Own theme values in app/theme.css, Tailwind mappings and the common two-pixel offset focus outline in app/index.css. Shared UI owns reusable presentation APIs without session, permission or RPC knowledge. Buttons, single-line inputs and native selects share sm/default/lg heights of 36/40/44 CSS pixels, corresponding font/padding/radius tokens, and a 40-pixel icon button. Default form controls use 40 pixels; parameter rows and compact list/result toolbars use 36; existing decision buttons use 44. Textareas remain multiline. Preserve native numeric input/select size attributes by naming the input presentation option controlSize. Minimum hit-target dimensions are 24 by 24 pixels; 44-pixel height is a deliberate comfortable option, not a claim that WCAG AA requires it universally.

Reuse one shared style contract for native controls instead of moving feature behavior into shared UI. Feature owners retain IDs, labels, parser callbacks, validation, lifecycle fences and authorization checks. No workflow redesign, new theme picker, blanket warning state, expanded role/Binding chooser, full palette ramp or typographic ratio is introduced. Retain the existing system fonts, page hierarchy and protected state disposal rules.

## Verification

Observe product browser scenarios fail before presentation changes. Qualify both light and dark themes: primary/rest/hover text, keyboard focus, readable invalid/refused feedback, identifiable field boundaries, matching default and compact control sizes, pending/disabled behavior, no write replay and revision-only draft retention. Cover authority loss and delayed replies, plus 320-pixel reflow. Keep existing real-backend authority scenarios in make verify; fixture scenarios establish browser presentation/state behavior only. Capture actual application screens for visual review and update affected README media. Require make verify, make supply-chain and staged review before commit; this does not establish complete WCAG conformance or M1 completion.

## Sources (verified 2026-10-21)

- [Reviewed Portcullis design proposal](https://claude.ai/artifact/4eioX5im7xkuxjpXWtVTag).
- [W3C text contrast](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html).
- [W3C non-text contrast](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html).
- [W3C focus visibility](https://www.w3.org/WAI/WCAG22/Understanding/focus-visible.html).
- [W3C target size](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html).
- [Kobalte Text Field semantics](https://kobalte.dev/docs/core/components/text-field/).
