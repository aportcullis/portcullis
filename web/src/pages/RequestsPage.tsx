import { useNavigate } from "@solidjs/router";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Button } from "@/shared/ui/button";
import type { Component } from "solid-js";
import { Show } from "solid-js";

import { hasPermission } from "@/entities/session/store";
import { RequestList } from "@/features/request/RequestList";

// RequestsPage is assembly only (frontend.md): the header and the create + list features. The route guard lives in AppShell; affordances are hidden by can() while the server keeps enforcing every RPC (ADR-0008).
const RequestsPage: Component = () => {
  const navigate = useNavigate();
  return (
  <>
    <PageHeader eyebrow="Governed access" title="Access requests"
      description="Follow requests from draft and review to a single approved execution."
      actions={<Show when={hasPermission("requests.create")}>
        <Button onClick={() => navigate("/requests/new")}>New request</Button>
      </Show>}
    />

    {/* The list is its own capability: a custom role may hold requests.create without requests.list (ADR-0008 allows any combination), and rendering the list anyway would fire an RPC that can only come back denied. */}
    <Show
      when={hasPermission("requests.list")}
      fallback={
        <p class="text-sm text-muted-foreground">
          You can submit requests here. Viewing existing requests needs the
          requests.list permission.
        </p>
      }
    >
      <div class="content-surface"><RequestList /></div>
    </Show>
  </>
);
};

export default RequestsPage;
