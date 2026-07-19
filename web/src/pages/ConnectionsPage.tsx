import type { Component } from "solid-js";
import { Show } from "solid-js";

import { can } from "@/entities/session/store";
import { ConnectionList } from "@/features/connection/ConnectionList";
import { CreateConnectionDialog } from "@/features/connection/CreateConnectionDialog";

// ConnectionsPage is assembly only (frontend.md): the header and the create +
// list features. The route guard lives in AppShell; affordances are hidden by
// can() (Me.permissions) while the server keeps enforcing every RPC
// (ADR-0008 — hiding is UX, not authorization).
const ConnectionsPage: Component = () => (
  <>
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Connections</h1>
        <p class="text-sm text-muted-foreground">
          Registered target databases. Credentials are encrypted and never displayed.
        </p>
      </div>
      <Show when={can("connections.create")}>
        <CreateConnectionDialog />
      </Show>
    </div>

    <ConnectionList />
  </>
);

export default ConnectionsPage;
