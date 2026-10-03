# text-field

Keep labels, description, input and error within the same Kobalte TextField root for accessible associations. TextFieldInput defaults to type="text". Use validationState="invalid" on the root for invalid input; business validation belongs to the caller.

## Use

```tsx
import { TextField, TextFieldInput, TextFieldLabel, TextFieldDescription, TextFieldErrorMessage } from "@/shared/ui/text-field";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [text-field.tsx](text-field.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
