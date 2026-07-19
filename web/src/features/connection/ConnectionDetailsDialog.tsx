import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import type { Connection } from "@/gen/portcullis/v1/connections_pb";
import { getConnection } from "@/entities/connection/store";
import { createOpenFetch } from "@/features/connection/openFetch";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/shared/ui/dialog";

// ConnectionDetailsDialog intentionally reads through connections.get only
// when opened. The list stays safe for list-only principals, while operators
// with connections.get can inspect the descriptor ADR-0014 requires the UI to
// expose. Credentials are not part of the response.
export const ConnectionDetailsDialog: Component<{ id: string; displayName: string }> = (props) => {
  const [connection, setConnection] = createSignal<Connection>();
  const fetch = createOpenFetch(() => getConnection(props.id), setConnection);

  const handleOpenChange = (next: boolean) => {
    if (next) setConnection(); // clear the previous open's data before loading
    fetch.handleOpenChange(next);
  };

  return (
    <Dialog open={fetch.open()} onOpenChange={handleOpenChange}>
      <DialogTrigger as={Button} size="sm" variant="outline">
        Details
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Connection details — “{props.displayName}”</DialogTitle>
          <DialogDescription>Target descriptor only. Credentials are never displayed.</DialogDescription>
        </DialogHeader>
        <Show when={fetch.loading()}><p class="text-sm text-muted-foreground">Loading…</p></Show>
        <Show when={fetch.error() !== ""}>
          <Alert variant="destructive"><AlertDescription>{fetch.error()}</AlertDescription></Alert>
        </Show>
        <Show when={connection()}>
          {(conn) => (
            <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
              <dt class="text-muted-foreground">Environment</dt><dd>{conn().environment}</dd>
              <Show when={conn().description !== ""}>
                <dt class="text-muted-foreground">Description</dt>
                <dd class="whitespace-pre-wrap">{conn().description}</dd>
              </Show>
              <dt class="text-muted-foreground">Host</dt><dd>{conn().host}</dd>
              <dt class="text-muted-foreground">Port</dt><dd>{conn().port}</dd>
              <dt class="text-muted-foreground">Database</dt><dd>{conn().database}</dd>
              <dt class="text-muted-foreground">TLS mode</dt><dd>{conn().tlsMode}</dd>
              <dt class="text-muted-foreground">Fingerprint</dt><dd class="break-all font-mono text-xs">{conn().targetFingerprint}</dd>
            </dl>
          )}
        </Show>
      </DialogContent>
    </Dialog>
  );
};
