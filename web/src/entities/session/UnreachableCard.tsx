import type { Component } from "solid-js";
import { createSignal } from "solid-js";

import { load, session } from "@/entities/session/store";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/shared/ui/card";

/** UnreachableCard recovers session or access-information outages through Me without replaying credentials. */
export const UnreachableCard: Component = () => {
  const [pending, setPending] = createSignal(false);
  const authorizationUnavailable = () => { const principal = session(); return principal.status === "unreachable" && principal.cause === "authorization"; };
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
          <CardTitle>{authorizationUnavailable() ? "Access information is temporarily unavailable" : "Portcullis is temporarily unreachable"}</CardTitle>
          <CardDescription>
            {authorizationUnavailable() ? "We could not load your access information. Retry to check your current access." : "The server could not be reached. Your session is unchanged — retry in a moment."}
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
