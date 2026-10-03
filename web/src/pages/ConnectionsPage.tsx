import type { Component } from "solid-js";
import { Show } from "solid-js";

import { PageHeader } from "@/shared/ui/PageHeader";
import { hasPermission } from "@/entities/session/store";
import { ConnectionList } from "@/features/connection/ConnectionList";
import { CreateConnectionForm } from "@/features/connection/CreateConnectionForm";

// ConnectionsPage is assembly only (frontend.md): the header and the create + list features. The route guard lives in AppShell; affordances are hidden by can() (Me.permissions) while the server keeps enforcing every RPC (ADR-0008 — hiding is UX, not authorization).
const ConnectionsPage: Component = () => (
  <>
    <PageHeader eyebrow="Database governance" title="Connections" description="Register the databases your team works with, then define how access is reviewed." />
    <div class="flex flex-col gap-4">
      <Show when={hasPermission("connections.create")}>
        <CreateConnectionForm />
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
      <div class="content-surface"><ConnectionList /></div>
    </Show>
  </>
);

export default ConnectionsPage;
