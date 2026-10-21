import { cn } from "@/shared/lib/utils";
import { nativeControlVariants } from "@/shared/ui/control";

/** Full-width select for request form fields. */
export const formSelectClass = cn(nativeControlVariants(), "w-full");

/** Compact select for the request list filter. */
export const filterSelectClass = cn(nativeControlVariants({ size: "sm" }), "w-44");
