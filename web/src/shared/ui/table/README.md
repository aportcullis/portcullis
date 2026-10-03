# table

Compose semantic HTML table parts with a caption and scoped headers. The wrapper scrolls horizontally. These primitives do not sort, paginate, fetch or virtualize rows; domain features supply bounded data and ordering.

## Use

```tsx
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell, TableCaption } from "@/shared/ui/table";
```

See [the type-checked example](example.tsx) for composition. The [public entry point](index.ts) lists supported exports; infer additional prop types with SolidJS `ComponentProps` when no named props type is exported.

## Customize and reuse

Edit [table.tsx](table.tsx) for implementation and styles. Import the directory entry point from consumers; do not import implementation files or examples. Preserve reactive props and accessibility behavior.

See the [UI kit guide](../README.md) for shared CSS, dependencies, license notices and verification.
