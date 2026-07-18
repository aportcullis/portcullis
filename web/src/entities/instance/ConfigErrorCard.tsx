import type { Component } from "solid-js";
import { createSignal } from "solid-js";

import { refetchLoginConfig } from "@/entities/instance/config";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/shared/ui/card";

// ConfigErrorCard renders when the initial GetConfig fails: without it, reading
// the errored resource throws and the login/bootstrap screen breaks with no
// recovery. Retry refetches the install config; on success the pages route as
// usual. Mirrors the session UnreachableCard so a server blip looks the same
// pre- and post-login.
export const ConfigErrorCard: Component = () => {
  const [pending, setPending] = createSignal(false);
  const retry = async () => {
    setPending(true);
    try {
      await refetchLoginConfig();
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
            The server could not be reached to load the sign-in page. Retry in a moment.
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
