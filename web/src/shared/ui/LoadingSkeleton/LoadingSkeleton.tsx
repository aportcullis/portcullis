import type { Component } from "solid-js";

/** Shows a bounded loading placeholder without exposing decorative bars to assistive technology. */
export const LoadingSkeleton: Component<{ label: string }> = (props) => (
  <div role="status" aria-busy="true" class="loading-skeleton content-surface">
    <span class="text-sm text-muted-foreground">{props.label}</span>
    <div aria-hidden="true" class="flex flex-col gap-3">
      <div class="h-4 w-2/3 rounded bg-muted" />
      <div class="h-4 w-full rounded bg-muted" />
      <div class="h-4 w-4/5 rounded bg-muted" />
    </div>
  </div>
);
