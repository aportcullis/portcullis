import type { Component } from "solid-js";

import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/shared/ui/card";

/** Replaces a page that failed to render with a generic message and recovery actions. */
export const ApplicationErrorCard: Component<{ onRetry: () => void }> = (props) => (
  <main class="flex min-h-screen items-center justify-center p-4">
    <Card class="w-full max-w-sm" role="alert">
      <CardHeader>
        <CardTitle>Something went wrong</CardTitle>
        <CardDescription>
          This page could not be displayed. Your session and saved work are unchanged.
        </CardDescription>
      </CardHeader>
      <CardContent class="flex gap-2">
        <Button onClick={() => props.onRetry()}>Try again</Button>
        <Button variant="outline" onClick={() => window.location.assign("/")}>Go to start page</Button>
      </CardContent>
    </Card>
  </main>
);
