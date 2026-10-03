import { A } from "@solidjs/router";
import type { Component } from "solid-js";
import { Show, createSignal, onCleanup } from "solid-js";

import {
  cancelAccessRequest,
  errorMessage,
  submitAccessRequest,
  loadAccessRequests,
} from "@/entities/request/store";
import type { AccessRequest } from "@/gen/portcullis/v1/access_requests_pb";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { hasPermission, session } from "@/entities/session/store";
import { mayCancel, maySubmitDraft } from "@/features/request/actions";
import { Button, buttonVariants } from "@/shared/ui/button";
import { createDialogSession } from "@/shared/lib/dialogSession";
import { executionsClient } from "@/shared/api/client";

// Offers owner actions according to request state and each operation's permission.
export const RequestRowActions: Component<{ request: AccessRequest; onChanged?: () => void; executionOnly?: boolean }> = (props) => {
  const { discardSession, runInSession, captureSession } = createDialogSession();
  onCleanup(discardSession);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");

  const isOwner = () => {
    const s = session();
    return s.status === "authenticated" && s.user.id === props.request.requester?.id;
  };
  const showSubmit = () => !props.executionOnly && maySubmitDraft(props.request, isOwner(), hasPermission);
  const showCancel = () => !props.executionOnly && mayCancel(props.request, isOwner(), hasPermission);
  const showExecute = () => isOwner() && hasPermission("requests.execute") && props.request.effectiveState === AccessRequestState.APPROVED;
  const showStop = () => isOwner() && hasPermission("requests.execute") && props.request.effectiveState === AccessRequestState.EXECUTING;
  const showResult = () => isOwner() && hasPermission("requests.get") && [AccessRequestState.SUCCEEDED, AccessRequestState.FAILED, AccessRequestState.OUTCOME_UNKNOWN].includes(props.request.effectiveState);

  const act = async (work: () => Promise<unknown>) => {
    setError("");
    setBusy(true);
    const outcome = await runInSession(work);
    if (outcome.status === "superseded") return;
    setBusy(false);
    if (outcome.status === "failed") setError(errorMessage(outcome.error));
  };

  return (
    <Show when={showSubmit() || showCancel() || showExecute() || showStop() || showResult()}>
      <span class="inline-flex items-center gap-2">
        <Show when={showExecute()}>
          <Button size="sm" disabled={busy()} onClick={() => void act(async () => {
            const principal = session(); const same = captureSession();
            try { await executionsClient.execute({ requestId: props.request.id }); }
            finally { if (same() && session() === principal) { if (hasPermission("requests.list")) await loadAccessRequests(); props.onChanged?.(); } }
          })}>Execute</Button>
        </Show>
        <Show when={showStop()}>
          <Button size="sm" variant="outline" disabled={busy()} onClick={() => void act(async () => { const principal = session(); const same = captureSession(); await executionsClient.cancel({ requestId: props.request.id }); if (same() && session() === principal) props.onChanged?.(); })}>Stop execution</Button>
        </Show>
        <Show when={showResult()}>
          <A class={buttonVariants({ size: "sm", variant: "outline" })} href={`/requests/${props.request.id}/result`}>Result</A>
        </Show>
        <Show when={error() !== ""}>
          <span class="text-sm text-destructive">{error()}</span>
        </Show>
        <Show when={showSubmit()}>
          <Button
            size="sm"
            disabled={busy()}
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
            disabled={busy()}
            onClick={() => void act(() => cancelAccessRequest(props.request.id))}
          >
            Cancel
          </Button>
        </Show>
      </span>
    </Show>
  );
};
