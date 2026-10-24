import type { ComponentProps } from "solid-js";
import { splitProps } from "solid-js";
import * as PopoverPrimitive from "@kobalte/core/popover";

import { cn } from "@/shared/lib/utils";

/** Controls an anchored disclosure without changing its document position. */
export const Popover = PopoverPrimitive.Root;
/** Renders the native button that toggles its anchored content. */
export const PopoverTrigger = PopoverPrimitive.Trigger;

/** Defines the native content attributes and primitive focus callbacks. */
export type PopoverContentProps = ComponentProps<"div"> & PopoverPrimitive.PopoverContentOptions;

/** Displays anchored content inline while preserving the primitive's dismissal handlers. */
export function PopoverContent(props: PopoverContentProps) {
  const [local, others] = splitProps(props, ["class"]);
  return (
    <PopoverPrimitive.Content
      class={cn(
        "z-50 w-72 max-w-[calc(100vw-2rem)] rounded-lg border border-border bg-popover p-4 text-popover-foreground shadow-md",
        local.class,
      )}
      {...others}
    />
  );
}
