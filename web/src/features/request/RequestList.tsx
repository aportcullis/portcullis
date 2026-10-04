import { A } from "@solidjs/router";
import type { Component } from "solid-js";
import { For, Show, createEffect, createSignal, on, onMount, onCleanup } from "solid-js";

import { actorLabel, requestStateFilterOptions, shouldPollRequestList, stateBadge, stateLabel } from "@/entities/request/model";
import { hasPermission, session } from "@/entities/session/store";
import { createRowActionRegistry } from "@/features/request/rowActionState";
import { filterSelectClass } from "@/features/request/selectStyles";
import { rangeEnd, rangeStart } from "@/entities/request/pagination";
import {
  goToPage,
  listError,
  listStale,
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
import { RequestWorkflow } from "@/features/request/RequestWorkflow";
import { RequestRowActions } from "@/features/request/RequestRowActions";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Badge } from "@/shared/ui/badge";
import { LoadingSkeleton } from "@/shared/ui/LoadingSkeleton";
import { Button, buttonVariants } from "@/shared/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";



// RequestList owns viewing the access requests: it fetches on mount, renders the table with state badges, and provides the filter and explicit page controls (§7.1). The details page carries the approve/reject/cancel affordances.
export const RequestList: Component = () => {
  const [expandedId, setExpandedId] = createSignal<string>();
  // Row action state lives at list level, keyed by request ID: a refresh replaces row objects, and <For> recreates their components.
  const rowActions = createRowActionRegistry();
  createEffect(on(session, () => rowActions.clear(), { defer: true }));
  onMount(() => {
    void loadAccessRequests();
    const refresh = () => { if (!document.hidden) void loadAccessRequests(); };
    // The timer polls the first page, where new requests arrive, and other pages only while a shown request can still change; returning to the tab or reconnecting always refreshes.
    const timer = setInterval(() => { if (shouldPollRequestList(accessRequests(), page())) refresh(); }, 30_000);
    window.addEventListener("online", refresh);
    document.addEventListener("visibilitychange", refresh);
    onCleanup(() => {
      clearInterval(timer);
      window.removeEventListener("online", refresh);
      document.removeEventListener("visibilitychange", refresh);
    });
  });
  const start = () => rangeStart(page(), pageSize(), totalCount());
  const end = () => rangeEnd(page(), pageSize(), accessRequests().length, totalCount());

  return (
    <div class="flex min-w-0 flex-col gap-4">
      <div class="flex flex-wrap items-center gap-2">
        <label class="text-sm text-muted-foreground" for="req-filter">
          Filter
        </label>
        <select
          id="req-filter"
          class={filterSelectClass}
          value={stateFilter()}
          onChange={(event) => setFilter(event.currentTarget.value)}
        >
          <option value="">All states</option>
          <For each={requestStateFilterOptions}>{(option) => <option value={option.value}>{option.label}</option>}</For>
        </select>
      </div>

      <Show when={listError() !== ""}>
        <Alert variant="destructive">
          <AlertDescription>
            <Show when={listStale()} fallback={listError()}>
              Refresh failed: {listError()} Showing the last loaded requests.
            </Show>
          </AlertDescription>
        </Alert>
      </Show>

      <Show
        when={accessRequests().length > 0}
        fallback={
          <Show
            when={listState() === "ready"}
            fallback={
              <Show when={listError() === ""}>
                <LoadingSkeleton label="Loading requests…" />
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
              <TableHead>Title</TableHead>
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
              {(accessRequest) => (
                <>
                <TableRow>
                  <TableCell class="max-w-80 break-words font-medium"><button class="inline-flex items-start gap-2 text-left text-primary underline-offset-4 hover:underline" aria-expanded={expandedId() === accessRequest.id} aria-controls={`workflow-${accessRequest.id}`} onClick={() => setExpandedId(expandedId() === accessRequest.id ? undefined : accessRequest.id)}><span aria-hidden="true">{expandedId() === accessRequest.id ? "▾" : "▸"}</span><span>{accessRequest.title || "Untitled request"}</span></button></TableCell>
                  <TableCell class="font-medium">{accessRequest.connectionName}</TableCell>
                  <TableCell class="text-muted-foreground">{actorLabel(accessRequest.requester)}</TableCell>
                  <TableCell class="text-muted-foreground">{accessRequest.statementClass || "—"}</TableCell>
                  <TableCell>
                    <Badge variant={stateBadge(accessRequest.effectiveState)}>{stateLabel(accessRequest.effectiveState)}</Badge>
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    {accessRequest.validApprovals.toString()} / {accessRequest.requiredApprovals}
                  </TableCell>
                  <TableCell class="text-right">
                    <span class="inline-flex items-center gap-2">
                      {/* Details reads through Get, which the server gates on requests.get — a role with list but not get would otherwise get a button that only ever fails. The owner's Submit/Cancel need requests.create instead, so they hang off the row itself. */}
                      <Show when={hasPermission("requests.get")}>
                        <A class={buttonVariants({ variant: "ghost", size: "sm" })} href={`/requests/${accessRequest.id}`}>
                          Details
                        </A>
                      </Show>
                      <RequestRowActions request={accessRequest} rowActions={rowActions} />
                    </span>
                  </TableCell>
                </TableRow>
                <Show when={expandedId() === accessRequest.id}><TableRow><TableCell colSpan={7} class="bg-muted/30"><RequestWorkflow request={accessRequest} mayOpenDetails={hasPermission("requests.get")} /></TableCell></TableRow></Show>
                </>
              )}
            </For>
          </TableBody>
        </Table>

        <div class="mt-4 flex flex-wrap items-center justify-between gap-3">
          <span class="text-sm text-muted-foreground">
            {start()}–{end()} of {totalCount().toString()}
          </span>
          <div class="flex flex-wrap items-center gap-2">
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

    </div>
  );
};
