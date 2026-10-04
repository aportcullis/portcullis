import { A } from "@solidjs/router";
import type { Component } from "solid-js";
import { Show, onCleanup } from "solid-js";

import {
  cancelAccessRequest,
  errorMessage,
  submitAccessRequest,
  loadAccessRequests,
} from "@/entities/request/store";
import { isExecutionOutcomeState } from "@/entities/request/model";
import type { AccessRequest } from "@/gen/portcullis/v1/access_requests_pb";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { hasPermission, session } from "@/entities/session/store";
import { mayCancel, maySubmitDraft } from "@/features/request/actions";
import type { RowActionRegistry } from "@/features/request/rowActionState";
import { createRowActionRegistry } from "@/features/request/rowActionState";
import { Button, buttonVariants } from "@/shared/ui/button";
import { executionsClient } from "@/shared/api/client";

// Offers owner actions according to request state and each operation's permission.
export const RequestRowActions: Component<{
  request: AccessRequest;
  onChanged?: () => void;
  executionOnly?: boolean;
  // The list owns this so pending and error state survive a refresh that recreates the row.
  rowActions?: RowActionRegistry;
}> = (props) => {
  const rowActions = props.rowActions ?? createRowActionRegistry();
  let mounted = true;
  onCleanup(() => {
    mounted = false;
  });
  const actionState = () => rowActions.stateFor(props.request.id);

  const isOwner = () => {
    const principal = session();
    return principal.status === "authenticated" && principal.user.id === props.request.requester?.id;
  };
  const showSubmit = () => !props.executionOnly && maySubmitDraft(props.request, isOwner(), hasPermission);
  const showCancel = () => !props.executionOnly && mayCancel(props.request, isOwner(), hasPermission);
  const showExecute = () => isOwner() && hasPermission("requests.execute") && props.request.effectiveState === AccessRequestState.APPROVED;
  const showStop = () => isOwner() && hasPermission("requests.execute") && props.request.effectiveState === AccessRequestState.EXECUTING;
  const showResult = () => isOwner() && hasPermission("requests.get") && isExecutionOutcomeState(props.request.effectiveState);

  // Settles against the request ID rather than this component, which a list refresh may already have replaced; a principal change discards the outcome.
  const act = async (work: () => Promise<unknown>) => {
    const requestId = props.request.id;
    const principal = session();
    const attempt = rowActions.begin(requestId);
    try {
      await work();
      if (session() === principal) rowActions.settle(requestId, attempt, "");
    } catch (error: unknown) {
      if (session() === principal) rowActions.settle(requestId, attempt, errorMessage(error));
    }
  };

  // Reports the change to the owning page while it is still shown, after the list refresh that may recreate this row.
  const refreshAfterExecution = async (principal: ReturnType<typeof session>) => {
    if (session() !== principal) return;
    if (hasPermission("requests.list")) await loadAccessRequests();
    if (mounted && session() === principal) props.onChanged?.();
  };

  return (
    <Show when={showSubmit() || showCancel() || showExecute() || showStop() || showResult()}>
      <span class="inline-flex items-center gap-2">
        <Show when={showExecute()}>
          <Button size="sm" disabled={actionState().busy} onClick={() => void act(async () => {
            const principal = session();
            try { await executionsClient.execute({ requestId: props.request.id }); }
            finally { await refreshAfterExecution(principal); }
          })}>Execute</Button>
        </Show>
        <Show when={showStop()}>
          <Button size="sm" variant="outline" disabled={actionState().busy} onClick={() => void act(async () => {
            const principal = session();
            await executionsClient.cancel({ requestId: props.request.id });
            if (mounted && session() === principal) props.onChanged?.();
          })}>Stop execution</Button>
        </Show>
        <Show when={showResult()}>
          <A class={buttonVariants({ size: "sm", variant: "outline" })} href={`/requests/${props.request.id}/result`}>Result</A>
        </Show>
        <Show when={actionState().error !== ""}>
          <span role="alert" class="text-sm text-destructive">{actionState().error}</span>
        </Show>
        <Show when={showSubmit()}>
          <Button
            size="sm"
            disabled={actionState().busy}
            onClick={() =>
              void act(() => submitAccessRequest(props.request.id, props.request.version))
            }
          >
            Submit
          </Button>
        </Show>
        <Show when={showCancel()}>
          <Button
            size="sm"
            variant="outline"
            disabled={actionState().busy}
            onClick={() => void act(() => cancelAccessRequest(props.request.id))}
          >
            Cancel
          </Button>
        </Show>
      </span>
    </Show>
  );
};
