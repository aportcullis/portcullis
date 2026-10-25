import { A } from "@solidjs/router";
import type { Component } from "solid-js";
import { Show, createMemo, onCleanup } from "solid-js";

import {
  cancelAccessRequest,
  submitAccessRequest,
  loadAccessRequests,
} from "@/entities/request/store";
import { errorMessage } from "@/shared/api/errors";
import { isExecutionOutcomeState } from "@/entities/request/model";
import type { ValidatedAccessRequest } from "@/shared/api/accessRequestValidation";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { hasPermission, session, sessionAuthorizationKey } from "@/entities/session/store";
import { mayCancel, maySubmitDraft } from "@/features/request/actions";
import type { RowActionOperation, RowActionRegistry } from "@/features/request/rowActionState";
import { createRowActionRegistry } from "@/features/request/rowActionState";
import { Button, buttonVariants } from "@/shared/ui/button";
import { executionsClient } from "@/shared/api/client";
import { createOwnedInteractionAuthorizationContext } from "@/shared/api/authorization";

export type RequestRowActionScope = { kind: "list"; registry: RowActionRegistry } |
  { kind: "details"; refresh: () => void };

export type RequestRowActionsProps = {
  request: ValidatedAccessRequest;
  scope: RequestRowActionScope;
};

export const RequestRowActions: Component<RequestRowActionsProps> = (props) => {
  const rowActions = createMemo(() => {
    const { scope } = props;
    if (scope.kind === "list") {
      const { registry } = scope;
      return registry;
    }
    const registry = createRowActionRegistry();
    const { dispose } = registry;
    onCleanup(dispose);
    return registry;
  });
  let mounted = true;
  onCleanup(() => {
    mounted = false;
  });
  const actionState = () => {
    const { id } = props.request;
    const { stateFor } = rowActions();
    return stateFor(id);
  };
  const stopState = () => {
    const { id } = props.request;
    const { stateFor } = rowActions();
    return stateFor(id, "stopExecution");
  };

  const isOwner = () => {
    const principal = session();
    if (principal.status !== "authenticated") return false;
    const { user: { id: userId } } = principal;
    const { requester: { id: requesterId } } = props.request;
    return userId === requesterId;
  };
  const showSubmit = () => {
    const { request, scope } = props;
    return scope.kind === "list" && maySubmitDraft(request, isOwner(), hasPermission);
  };
  const showCancel = () => {
    const { request, scope } = props;
    return scope.kind === "list" && mayCancel(request, isOwner(), hasPermission);
  };
  const showExecute = () => {
    const { effectiveState } = props.request;
    return isOwner() && hasPermission("requests.execute") && effectiveState === AccessRequestState.APPROVED;
  };
  const showStop = () => {
    const { effectiveState } = props.request;
    return isOwner() && hasPermission("requests.execute") && effectiveState === AccessRequestState.EXECUTING;
  };
  const showResult = () => {
    const { effectiveState } = props.request;
    return isOwner() && hasPermission("requests.get") && isExecutionOutcomeState(effectiveState);
  };

  const runRequestAction = async (
    operation: RowActionOperation,
    work: (requestId: string, isCurrentAction: () => boolean) => Promise<unknown>,
  ): Promise<void> => {
    const { begin, settle, isCurrentAttempt } = rowActions();
    const { id: requestId } = props.request;
    const authorizationKey = sessionAuthorizationKey();
    const admission = begin(requestId, operation);
    if (admission.kind !== "started") return;
    const { attempt } = admission;
    const isCurrentAction = () => sessionAuthorizationKey() === authorizationKey &&
      isCurrentAttempt(requestId, attempt, operation);
    try {
      await work(requestId, isCurrentAction);
      if (isCurrentAction()) settle(requestId, attempt, "", operation);
    } catch (error: unknown) {
      if (isCurrentAction()) settle(requestId, attempt, errorMessage(error), operation);
    }
  };

  const refreshAfterExecution = (requestId: string, isCurrentAction: () => boolean): void => {
    if (!isCurrentAction()) return;
    const { scope, request: { id } } = props;
    if (mounted && id === requestId && scope.kind === "details") {
      const { refresh } = scope;
      refresh();
    }
    // List row replacement keeps its owner alive; leaving that list disposes its registry.
    if (isCurrentAction() && hasPermission("requests.list")) void loadAccessRequests();
  };

  const captureExecutionAuthorizationContext = (isCurrentAction: () => boolean) => {
    const startingSession = session();
    return createOwnedInteractionAuthorizationContext(() => isCurrentAction() && session() === startingSession);
  };

  const executeRequest = (): Promise<void> => runRequestAction("request", async (requestId, isCurrentAction) => {
    try {
      await executionsClient.execute(
        { requestId },
        { contextValues: captureExecutionAuthorizationContext(isCurrentAction) },
      );
    } finally {
      refreshAfterExecution(requestId, isCurrentAction);
    }
  });

  const stopExecution = (): Promise<void> => runRequestAction("stopExecution", async (requestId, isCurrentAction) => {
    await executionsClient.cancel(
      { requestId },
      { contextValues: captureExecutionAuthorizationContext(isCurrentAction) },
    );
    refreshAfterExecution(requestId, isCurrentAction);
  });

  const submitRequest = (): Promise<void> => {
    const { version } = props.request;
    return runRequestAction("request", requestId => submitAccessRequest(requestId, version));
  };

  const cancelRequest = (): Promise<void> => runRequestAction("request", requestId => cancelAccessRequest(requestId));

  return (
    <Show when={showSubmit() || showCancel() || showExecute() || showStop() || showResult()}>
      <span class="inline-flex items-center gap-2">
        <Show when={showExecute()}>
          <Button
            size="sm"
            disabled={actionState().busy}
            onClick={() => void executeRequest()}
          >
            Execute
          </Button>
        </Show>
        <Show when={showStop()}>
          <Button
            size="sm"
            variant="outline"
            disabled={stopState().busy}
            onClick={() => void stopExecution()}
          >
            Stop execution
          </Button>
        </Show>
        <Show when={showResult()}>
          <A
            class={buttonVariants({ size: "sm", variant: "outline" })}
            href={`/requests/${props.request.id}/result`}
          >
            Result
          </A>
        </Show>
        <Show when={actionState().error !== ""}>
          <span role="alert" class="text-sm text-destructive-text">
            {actionState().error}
          </span>
        </Show>
        <Show when={stopState().error !== ""}>
          <span role="alert" class="text-sm text-destructive-text">
            {stopState().error}
          </span>
        </Show>
        <Show when={showSubmit()}>
          <Button
            size="sm"
            disabled={actionState().busy}
            onClick={() => void submitRequest()}
          >
            Submit
          </Button>
        </Show>
        <Show when={showCancel()}>
          <Button
            size="sm"
            variant="outline"
            disabled={actionState().busy}
            onClick={() => void cancelRequest()}
          >
            Cancel
          </Button>
        </Show>
      </span>
    </Show>
  );
};
