import type { Component } from "solid-js";

/** Defines the inputs owned by LoadingSkeleton. */
export type LoadingSkeletonProps = {
  label: string;
};

/** Shows a bounded loading placeholder without exposing decorative bars to assistive technology. */
export const LoadingSkeleton: Component<LoadingSkeletonProps> = (props) => (
  <div role="status" aria-busy="true" class="loading-skeleton content-surface">
    <span class="text-sm text-muted-foreground">{props.label}</span>
    <div aria-hidden="true" class="flex flex-col gap-3">
      <div class="h-4 w-2/3 rounded bg-muted" />
      <div class="h-4 w-full rounded bg-muted" />
      <div class="h-4 w-4/5 rounded bg-muted" />
    </div>
  </div>
);
