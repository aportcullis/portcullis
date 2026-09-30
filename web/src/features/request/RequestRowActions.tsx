import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import {
  cancelAccessRequest,
  errorMessage,
  submitAccessRequest,
} from "@/entities/request/store";
import type { AccessRequest } from "@/gen/portcullis/v1/access_requests_pb";
import { hasPermission, session } from "@/entities/session/store";
import { mayCancel, maySubmitDraft } from "@/features/request/actions";
import { Button } from "@/shared/ui/button";

// Gate row Submit/Cancel on requests.create and Details on requests.get independently; editing needs the decrypted detail payload.
export const RequestRowActions: Component<{ request: AccessRequest }> = (props) => {
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");

  const isOwner = () => {
    const s = session();
    return s.status === "authenticated" && s.user.id === props.request.requester?.id;
  };
  const showSubmit = () => maySubmitDraft(props.request, isOwner(), hasPermission);
  const showCancel = () => mayCancel(props.request, isOwner(), hasPermission);

  const act = async (work: () => Promise<unknown>) => {
    setError("");
    setBusy(true);
    try {
      await work();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Show when={showSubmit() || showCancel()}>
      <span class="inline-flex items-center gap-2">
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
