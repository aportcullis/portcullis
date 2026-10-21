import { cva } from "class-variance-authority";

/** Shared single-line geometry for buttons, native selects and text inputs. */
export const controlSizes = {
  default: "h-[var(--control-height)] px-4 text-sm",
  sm: "h-[var(--control-height-sm)] px-3 text-xs",
  lg: "h-[var(--control-height-lg)] px-4 text-sm",
};

/** Names the shared compact, default and large control geometry. */
export type ControlSize = keyof typeof controlSizes;

/** Native control presentation; callers retain labels, values, events and validation. */
export const nativeControlVariants = cva(
  "min-w-0 max-w-full rounded-md border border-input bg-background text-foreground disabled:cursor-not-allowed disabled:opacity-50",
  { variants: { size: controlSizes }, defaultVariants: { size: "default" } },
);
