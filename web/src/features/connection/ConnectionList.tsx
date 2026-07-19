import type { Component } from "solid-js";
import { For, Show, onMount } from "solid-js";

import { connections, listError, listState, loadConnections } from "@/entities/connection/store";
import { can } from "@/entities/session/store";
import { ArchiveConnectionDialog } from "@/features/connection/ArchiveConnectionDialog";
import { ConnectionDetailsDialog } from "@/features/connection/ConnectionDetailsDialog";
import { EditConnectionDialog } from "@/features/connection/EditConnectionDialog";
import { EditPolicyDialog } from "@/features/connection/EditPolicyDialog";
import { TestConnectionButton } from "@/features/connection/TestConnectionButton";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Badge } from "@/shared/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";

// ConnectionList owns "viewing the connections" end to end: it fetches on mount
// and renders the table plus the per-row actions. Fetching lives here (a
// feature), not in the page — the page is assembly only (frontend.md).
// Per-row actions are hidden by can() (Me.permissions) — affordance UX only,
// the server still authorizes every RPC (ADR-0008). Production rows carry a
// destructive-variant badge so the label is unmissable before any risky edit.
export const ConnectionList: Component = () => {
  onMount(() => void loadConnections());
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
          // The empty state renders only after a load actually SUCCEEDED — the
          // store starts empty, and showing "no connections" before the first
          // response would misinform an operator whose connections just have
          // not arrived yet (external review).
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
                      <Show when={can("connections.get")}>
                        <ConnectionDetailsDialog id={conn.id} displayName={conn.displayName} />
                      </Show>
                      <Show
                        when={!conn.archivedAt}
                        // An archived row keeps its descriptor editable — name,
                        // environment, and description label its history; test,
                        // config edit, and re-archive stay hidden.
                        fallback={
                          <Show when={can("connections.update")}>
                            <EditConnectionDialog id={conn.id} displayName={conn.displayName} environment={conn.environment} description={conn.description} archived />
                          </Show>
                        }
                      >
                        <span class="inline-flex items-center gap-2">
                          <Show when={can("policies.get")}>
                            <EditPolicyDialog id={conn.id} displayName={conn.displayName} />
                          </Show>
                          <Show when={can("connections.test")}>
                            <TestConnectionButton id={conn.id} />
                          </Show>
                          <Show when={can("connections.update")}>
                            <EditConnectionDialog id={conn.id} displayName={conn.displayName} environment={conn.environment} description={conn.description} />
                          </Show>
                          <Show when={can("connections.delete")}>
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
    </>
  );
};
