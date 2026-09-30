import type { Component } from "solid-js";

import { buttonVariants } from "@/shared/ui/button";
import { cn } from "@/shared/lib/utils";

// Google login is a server-side redirect dance (ADR-0007): a plain anchor to the Go route — deliberately NO Google JS SDK.
export const GoogleLoginButton: Component = () => (
  <a href="/auth/google/start" class={cn(buttonVariants({ variant: "outline" }), "w-full")}>
    Continue with Google
  </a>
);
