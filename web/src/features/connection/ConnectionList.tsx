import type { Component } from "solid-js";
import { For, Show, createSignal, onMount } from "solid-js";

import { connections, listError, listState, loadConnections } from "@/entities/connection/store";
import { hasPermission } from "@/entities/session/store";
import type { ConnectionSummary } from "@/gen/portcullis/v1/connections_pb";
import { ArchiveConnectionDialog } from "@/features/connection/ArchiveConnectionDialog";
import { ConnectionDetailsPanel } from "@/features/connection/ConnectionDetailsPanel";
import { EditConnectionPanel } from "@/features/connection/EditConnectionPanel";
import { EditPolicyPanel } from "@/features/connection/EditPolicyPanel";
import { resolveEditTarget } from "@/features/connection/editTarget";
import { TestConnectionButton } from "@/features/connection/TestConnectionButton";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";

// ConnectionList owns fetching and per-action permission affordances; production targets carry a prominent badge.
export const ConnectionList: Component = () => {
  onMount(() => void loadConnections());
  // Own draft dialogs outside Solid’s keyed rows so list refreshes preserve typed input.
  const [viewing, setViewing] = createSignal<ConnectionSummary>();
  const [editing, setEditing] = createSignal<ConnectionSummary | undefined>();
  const [editingPolicy, setEditingPolicy] = createSignal<ConnectionSummary | undefined>();

  return (
    <>
      <Show when={listError() !== ""}>
        <Alert variant="destructive">
          <AlertDescription>{listError()}</AlertDescription>
        </Alert>
      </Show>

      <Show
        when={connections().length > 0}
        fallback={
          // The empty state renders only after a load actually SUCCEEDED — the store starts empty, and showing "no connections" before the first response would misinform an operator whose connections just have not arrived yet.
          <Show
            when={listState() === "ready"}
            fallback={
              <Show when={listError() === ""}>
                <p class="text-sm text-muted-foreground">Loading connections…</p>
              </Show>
            }
          >
            <p class="text-sm text-muted-foreground">
              No connections yet. Register the first one to start governing access.
            </p>
          </Show>
        }
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Database type</TableHead>
              <TableHead>Environment</TableHead>
              <TableHead>Status</TableHead>
              <TableHead class="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <For each={connections()}>
              {(conn) => (
                <TableRow>
                  <TableCell class="font-medium">{conn.displayName}</TableCell>
                  <TableCell class="text-muted-foreground">{conn.dbType}</TableCell>
                  <TableCell>
                    <Badge variant={conn.environment === "production" ? "destructive" : "outline"}>
                      {conn.environment === "production" ? "production" : "development"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Badge variant={conn.archivedAt ? "outline" : "default"}>
                      {conn.archivedAt ? "archived" : "active"}
                    </Badge>
                  </TableCell>
                  <TableCell class="text-right">
                    <span class="inline-flex items-center gap-2">
                      <Show when={hasPermission("connections.get")}>
                        <Button size="sm" variant="outline" onClick={() => setViewing(conn)}>Details</Button>
                      </Show>
                      <Show
                        when={!conn.archivedAt}
                        // An archived row keeps its descriptor editable — name, environment, and description label its history; test, config edit, and re-archive stay hidden.
                        fallback={
                          <Show when={hasPermission("connections.update")}>
                            <Button size="sm" variant="outline" disabled={editing() !== undefined} onClick={() => setEditing(conn)}>
                              Edit
                            </Button>
                          </Show>
                        }
                      >
                        <span class="inline-flex items-center gap-2">
                          <Show when={hasPermission("policies.get")}>
                            <Button size="sm" variant="outline" disabled={editingPolicy() !== undefined} onClick={() => setEditingPolicy(conn)}>
                              Policy
                            </Button>
                          </Show>
                          <Show when={hasPermission("connections.test")}>
                            <TestConnectionButton id={conn.id} />
                          </Show>
                          <Show when={hasPermission("connections.update")}>
                            <Button size="sm" variant="outline" disabled={editing() !== undefined} onClick={() => setEditing(conn)}>
                              Edit
                            </Button>
                          </Show>
                          <Show when={hasPermission("connections.delete")}>
                            <ArchiveConnectionDialog id={conn.id} displayName={conn.displayName} />
                          </Show>
                        </span>
                      </Show>
                    </span>
                  </TableCell>
                </TableRow>
              )}
            </For>
          </TableBody>
        </Table>
      </Show>

      {/* Mounted here, not in a row: see the note on `editing` above. Each resolves its target by id against the current list, so a refresh hands the open form a fresh version token instead of destroying it. */}
      <ConnectionDetailsPanel target={viewing()} onClose={() => setViewing()} />
      <EditConnectionPanel
        target={resolveEditTarget(connections(), editing())}
        onClose={() => setEditing(undefined)}
      />
      <EditPolicyPanel
        target={resolveEditTarget(connections(), editingPolicy())}
        onClose={() => setEditingPolicy(undefined)}
      />
    </>
  );
};
