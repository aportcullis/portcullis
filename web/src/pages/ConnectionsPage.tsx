import type { Component } from "solid-js";
import { Show } from "solid-js";

import { PageHeader } from "@/shared/ui/PageHeader";
import { hasPermission, sessionAuthorizationKey } from "@/entities/session/store";
import { SessionInteraction } from "@/entities/session/SessionInteraction";
import { ConnectionList } from "@/features/connection/ConnectionList";
import { CreateConnectionForm } from "@/features/connection/CreateConnectionForm";

// ConnectionsPage assembles the header and connection features; Me.capabilities gates affordances while every RPC checks current server authority.
const ConnectionsPage: Component = () => (
  <SessionInteraction>
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
      <Show when={sessionAuthorizationKey()} keyed>{_authorization => <div class="content-surface"><ConnectionList /></div>}</Show>
    </Show>
  </SessionInteraction>
);

export default ConnectionsPage;
