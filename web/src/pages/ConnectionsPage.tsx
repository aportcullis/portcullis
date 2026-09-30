import type { Component } from "solid-js";
import { Show } from "solid-js";

import { hasPermission } from "@/entities/session/store";
import { ConnectionList } from "@/features/connection/ConnectionList";
import { CreateConnectionDialog } from "@/features/connection/CreateConnectionDialog";

// ConnectionsPage is assembly only (frontend.md): the header and the create + list features. The route guard lives in AppShell; affordances are hidden by can() (Me.permissions) while the server keeps enforcing every RPC (ADR-0008 — hiding is UX, not authorization).
const ConnectionsPage: Component = () => (
  <>
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Connections</h1>
        <p class="text-sm text-muted-foreground">
          Registered target databases. Credentials are encrypted and never displayed.
        </p>
      </div>
      <Show when={hasPermission("connections.create")}>
        <CreateConnectionDialog />
      </Show>
    </div>

    {/* The table is its own capability: a custom role may hold connections.create without connections.list (ADR-0008), and rendering the list anyway fires an RPC that can only come back denied — the same split RequestsPage already makes. */}
    <Show
      when={hasPermission("connections.list")}
      fallback={
        <p class="text-sm text-muted-foreground">
          You can register connections here. Viewing the existing ones needs the
          connections.list permission.
        </p>
      }
    >
      <ConnectionList />
    </Show>
  </>
);

export default ConnectionsPage;
