import type { Component } from "solid-js";
import { Match, Switch } from "solid-js";

import { Navigate } from "@solidjs/router";

import { UnreachableCard } from "@/entities/session/UnreachableCard";
import { session } from "@/entities/session/store";
import { ConnectionList } from "@/features/connection/ConnectionList";
import { CreateConnectionDialog } from "@/features/connection/CreateConnectionDialog";

// ConnectionsPage is assembly only (frontend.md): the route guard, the header,
// and the create + list features. Any authenticated user may open it; non-admins
// get the server's uniform "permission denied" (ADR-0008 — capabilities are not
// enumerated client-side; Me carries no permissions yet).
const ConnectionsPage: Component = () => (
  <Switch>
    <Match when={session().status === "anonymous"}>
      <Navigate href="/login" />
    </Match>
    <Match when={session().status === "unreachable"}>
      <UnreachableCard />
    </Match>
    <Match when={session().status === "authenticated"}>
      <main class="mx-auto flex min-h-screen w-full max-w-4xl flex-col gap-6 p-6">
        <div class="flex items-center justify-between">
          <div>
            <h1 class="text-2xl font-semibold tracking-tight">Connections</h1>
            <p class="text-sm text-muted-foreground">
              Registered target databases. Credentials are encrypted and never displayed.
            </p>
          </div>
          <CreateConnectionDialog />
        </div>

        <ConnectionList />

        <a class="text-sm text-muted-foreground underline-offset-4 hover:underline" href="/">
          ← Home
        </a>
      </main>
    </Match>
  </Switch>
);

export default ConnectionsPage;
