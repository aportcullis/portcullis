import type { Component } from "solid-js";
import { Show, createSignal, createEffect, createMemo, on, onCleanup } from "solid-js";

import type { Connection } from "@/gen/portcullis/v1/connections_pb";
import { getConnection } from "@/entities/connection/store";
import { errorMessage } from "@/shared/api/errors";
import { createOpenFetch } from "@/shared/lib/openFetch";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { InlinePanel } from "@/shared/ui/InlinePanel";

// ConnectionDetailsPanel intentionally reads through connections.get only when opened. The list stays safe for list-only principals, while operators with connections.get can inspect the descriptor ADR-0014 requires the UI to expose. Credentials are not part of the response.
export const ConnectionDetailsPanel: Component<{ target: { id: string; displayName: string } | undefined; onClose: () => void }> = (props) => {
  const [connection, setConnection] = createSignal<Connection>();
  const fetch = createOpenFetch(() => getConnection(props.target?.id ?? ""), setConnection, errorMessage);

  const handleOpenChange = (next: boolean) => {
    if (next) setConnection(); // clear the previous open's data before loading
    fetch.handleOpenChange(next);
    if (!next) props.onClose();
  };

  createEffect(on(createMemo(() => props.target?.id), id => handleOpenChange(id !== undefined)));
  onCleanup(() => fetch.handleOpenChange(false));
  return (
    <InlinePanel open={props.target !== undefined} label="Connection details" onClose={() => handleOpenChange(false)}>

        <header>
          <h2 class="text-xl font-semibold">Connection details — “{props.target?.displayName}”</h2>
          <p class="text-sm text-muted-foreground">Target descriptor only. Credentials are never displayed.</p>
        </header>
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

    </InlinePanel>
  );
};
