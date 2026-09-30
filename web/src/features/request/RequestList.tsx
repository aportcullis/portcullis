import type { Component } from "solid-js";
import { For, Show, createSignal, onMount } from "solid-js";

import type { AccessRequest } from "@/gen/portcullis/v1/access_requests_pb";

import { stateBadge, stateLabel } from "@/entities/request/model";
import { hasPermission } from "@/entities/session/store";
import { rangeEnd, rangeStart } from "@/entities/request/pagination";
import {
  goToPage,
  listError,
  listState,
  loadAccessRequests,
  page,
  pageSize,
  accessRequests,
  setFilter,
  stateFilter,
  totalCount,
  totalPages,
} from "@/entities/request/store";
import { RequestDetailsDialog } from "@/features/request/RequestDetailsDialog";
import { RequestRowActions } from "@/features/request/RequestRowActions";
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

const selectClass =
  "flex h-9 w-44 rounded-md border border-input bg-background px-3 py-1 text-sm";

const actorLabel = (a?: { displayName: string; email: string }): string =>
  a ? a.displayName || a.email : "—";

// The state filter offers the lifecycle states this slice produces; the executing/terminal-execution family arrives with the execution slice.
const filterStates = ["draft", "pending", "approved", "rejected", "expired", "cancelled"];

// RequestList owns viewing the access requests: it fetches on mount, renders the table with state badges, and provides the filter and explicit page controls (§7.1). The details dialog carries the approve/reject/cancel affordances.
export const RequestList: Component = () => {
  onMount(() => void loadAccessRequests());
  // Own the draft dialog outside keyed rows and resolve its target by ID so refreshes update data without destroying input.
  const [showing, setShowing] = createSignal<AccessRequest | undefined>();
  const shown = () => {
    const captured = showing();
    if (!captured) return undefined;
    // Falling back to the captured row keeps the dialog alive when a failed reload empties the list.
    return accessRequests().find((row) => row.id === captured.id) ?? captured;
  };

  const start = () => rangeStart(page(), pageSize(), totalCount());
  const end = () => rangeEnd(page(), pageSize(), accessRequests().length, totalCount());

  return (
    <>
      <div class="flex items-center gap-2">
        <label class="text-sm text-muted-foreground" for="req-filter">
          Filter
        </label>
        <select
          id="req-filter"
          class={selectClass}
          value={stateFilter()}
          onChange={(e) => setFilter(e.currentTarget.value)}
        >
          <option value="">All states</option>
          <For each={filterStates}>{(s) => <option value={s}>{s}</option>}</For>
        </select>
      </div>

      <Show when={listError() !== ""}>
        <Alert variant="destructive">
          <AlertDescription>{listError()}</AlertDescription>
        </Alert>
      </Show>

      <Show
        when={accessRequests().length > 0}
        fallback={
          <Show
            when={listState() === "ready"}
            fallback={
              <Show when={listError() === ""}>
                <p class="text-sm text-muted-foreground">Loading requests…</p>
              </Show>
            }
          >
            <p class="text-sm text-muted-foreground">No access requests yet.</p>
          </Show>
        }
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Connection</TableHead>
              <TableHead>Requester</TableHead>
              <TableHead>Class</TableHead>
              <TableHead>State</TableHead>
              <TableHead>Approvals</TableHead>
              <TableHead class="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <For each={accessRequests()}>
              {(r) => (
                <TableRow>
                  <TableCell class="font-medium">{r.connectionName}</TableCell>
                  <TableCell class="text-muted-foreground">{actorLabel(r.requester)}</TableCell>
                  <TableCell class="text-muted-foreground">{r.statementClass || "—"}</TableCell>
                  <TableCell>
                    <Badge variant={stateBadge(r.effectiveState)}>{stateLabel(r.effectiveState)}</Badge>
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    {r.validApprovals.toString()} / {r.requiredApprovals}
                  </TableCell>
                  <TableCell class="text-right">
                    <span class="inline-flex items-center gap-2">
                      {/* Details reads through Get, which the server gates on requests.get — a role with list but not get would otherwise get a button that only ever fails. The owner's Submit/Cancel need requests.create instead, so they hang off the row itself. */}
                      <Show when={hasPermission("requests.get")}>
                        <Button variant="ghost" size="sm" onClick={() => setShowing(r)}>
                          Details
                        </Button>
                      </Show>
                      <RequestRowActions request={r} />
                    </span>
                  </TableCell>
                </TableRow>
              )}
            </For>
          </TableBody>
        </Table>

        <div class="flex items-center justify-between">
          <span class="text-sm text-muted-foreground">
            {start()}–{end()} of {totalCount().toString()}
          </span>
          <div class="flex items-center gap-2">
            <Button variant="outline" size="sm" disabled={page() <= 1} onClick={() => goToPage(page() - 1)}>
              Previous
            </Button>
            <span class="text-sm text-muted-foreground">
              Page {page()} of {Math.max(totalPages(), 1)}
            </span>
            <Button
              variant="outline"
              size="sm"
              disabled={page() >= totalPages()}
              onClick={() => goToPage(page() + 1)}
            >
              Next
            </Button>
          </div>
        </div>
      </Show>

      {/* Mounted outside the table — see the note on `showing` above. */}
      <RequestDetailsDialog target={shown()} onClose={() => setShowing(undefined)} />
    </>
  );
};
