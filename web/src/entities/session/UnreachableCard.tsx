import type { Component } from "solid-js";
import { createSignal } from "solid-js";

import { load } from "@/entities/session/store";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/shared/ui/card";

// UnreachableCard renders when the session state is "unreachable": the server
// could not answer Me, which must not be presented as a logout (ADR-0006).
// Retry re-resolves the session; on success the route guards take over.
export const UnreachableCard: Component = () => {
  const [pending, setPending] = createSignal(false);
  const retry = async () => {
    setPending(true);
    try {
      await load();
    } finally {
      setPending(false);
    }
  };
  return (
    <main class="flex min-h-screen items-center justify-center p-4">
      <Card class="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Portcullis is temporarily unreachable</CardTitle>
          <CardDescription>
            The server could not be reached. Your session is unchanged — retry in a moment.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button disabled={pending()} onClick={retry}>
            {pending() ? "Retrying…" : "Retry"}
          </Button>
        </CardContent>
      </Card>
    </main>
  );
};
