# Native control presentation

`nativeControlVariants({ size: "sm" | "default" | "lg" })` styles native selects and single-line inputs at 36/40/44 CSS pixels. `controlSizes` supplies the same geometry to Button and TextFieldInput. Heights come from the application theme; multiline textareas keep their natural height.

Feature callers own labels, IDs, native numeric `size`, values, parsing, disabled state, validation and events. These styles contain no authorization or data loading. Keep a visible keyboard focus indicator through the application base stylesheet, and provide a visible label or accessible name. See [example.tsx](example.tsx).
