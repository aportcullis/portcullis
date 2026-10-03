# BrandLogo

Renders the Portcullis wordmark with meaningful alt text. Requires /brand/logo.svg and /brand/logo-dark.svg, and the configured dark variant. Apply .dark or data-kb-theme="dark" to select the dark asset. The class prop changes sizing. For another project, replace branding assets and alt text deliberately; see the repository branding guide.

## Use

```tsx
import { BrandLogo } from "@/shared/ui/BrandLogo";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [BrandLogo.tsx](BrandLogo.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
