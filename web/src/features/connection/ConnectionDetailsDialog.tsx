import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import type { Connection } from "@/gen/portcullis/v1/connections_pb";
import { errorMessage, getConnection } from "@/entities/connection/store";
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
  const [open, setOpen] = createSignal(false);
  const [connection, setConnection] = createSignal<Connection>();
  const [error, setError] = createSignal("");
  const [loading, setLoading] = createSignal(false);

  // Each open starts a fetch; a slow response from an earlier open must not
  // overwrite a later one's result or reset its loading state. The sequence
  // fences every callback to the open that started it.
  let seq = 0;

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) return;
    const s = ++seq;
    setConnection();
    setError("");
    setLoading(true);
    void getConnection(props.id)
      .then((result) => {
        if (s === seq) setConnection(result);
      })
      .catch((err: unknown) => {
        if (s === seq) setError(errorMessage(err));
      })
      .finally(() => {
        if (s === seq) setLoading(false);
      });
  };

  return (
    <Dialog open={open()} onOpenChange={handleOpenChange}>
      <DialogTrigger as={Button} size="sm" variant="outline">
        Details
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Connection details — “{props.displayName}”</DialogTitle>
          <DialogDescription>Target descriptor only. Credentials are never displayed.</DialogDescription>
        </DialogHeader>
        <Show when={loading()}><p class="text-sm text-muted-foreground">Loading…</p></Show>
        <Show when={error() !== ""}>
          <Alert variant="destructive"><AlertDescription>{error()}</AlertDescription></Alert>
        </Show>
        <Show when={connection()}>
          {(conn) => (
            <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
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
