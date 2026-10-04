import { cn } from "@/shared/lib/utils";

// Native selects match the vendored text inputs; the request forms and the list filter share one look.
const nativeSelectBase = "flex rounded-md border border-input bg-background px-3 text-sm";

/** Full-width select for request form fields. */
export const formSelectClass = cn(nativeSelectBase, "h-10 w-full py-2");

/** Compact select for the request list filter. */
export const filterSelectClass = cn(nativeSelectBase, "h-9 w-44 py-1");
