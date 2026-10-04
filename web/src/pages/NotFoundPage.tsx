import type { Component } from "solid-js";

import { A } from "@solidjs/router";

import { PageHeader } from "@/shared/ui/PageHeader";

/** Tells the caller that no page exists at the requested address. */
const NotFoundPage: Component = () => (
  <>
    <PageHeader title="Page not found" description="No page exists at this address. Check the link or continue from the start page." />
    <A href="/" class="w-fit text-sm underline underline-offset-4">Go to start page</A>
  </>
);

export default NotFoundPage;
