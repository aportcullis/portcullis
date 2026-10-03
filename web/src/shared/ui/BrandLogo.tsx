import type { Component } from "solid-js";

import { cn } from "@/shared/lib/utils";

/** Displays the shared Portcullis wordmark at the caller's layout size. */
export const BrandLogo: Component<{ class?: string }> = (props) => (
  <img
    src="/brand/logo.svg"
    alt="Portcullis"
    width="460"
    height="128"
    class={cn("h-10 w-auto shrink-0", props.class)}
  />
);
