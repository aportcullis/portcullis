import { create } from "@bufbuild/protobuf";
import type { Component } from "solid-js";
import { For, Show, createEffect, createMemo, createSignal, on } from "solid-js";

import type { AccessRequest, GetAccessRequestResponse } from "@/gen/portcullis/v1/access_requests_pb";
import { AccessRequestSchema } from "@/gen/portcullis/v1/access_requests_pb";
import { loginConfig } from "@/entities/instance/config";
import { stateBadge, stateLabel } from "@/entities/request/model";
import { truncateReasonCodePoints, countReasonCodePoints } from "@/entities/request/reason";
import {
  approveAccessRequest,
  cancelAccessRequest,
  submitAccessRequest,
  errorMessage,
  getAccessRequest,
  rejectAccessRequest,
  updateDraft,
} from "@/entities/request/store";
import { hasPermission, session } from "@/entities/session/store";
import { createOpenFetch } from "@/shared/lib/openFetch";
import {
  mayApprove,
  mayCancel,
  mayEditDraft,
  mayReject,
  maySubmitDraft,
} from "@/features/request/actions";
import type { RequestDraft } from "@/features/request/draft";
import { createRequestDraftFromPayload, toTypedRequestParameters, validateRequestDraft } from "@/features/request/draft";
import { ParamEditor } from "@/features/request/ParamEditor";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { TextField, TextFieldLabel, TextFieldTextArea } from "@/shared/ui/text-field";

// The stand-in for "no request is open" — see current() below.
const noRequest = create(AccessRequestSchema, {});

const actorLabel = (a?: { displayName: string; email: string }): string =>
  a ? a.displayName || a.email : "—";

// The list owns the draft-edit dialog so row refreshes preserve typed SQL. Server authorization controls payload visibility and mutations.
export const RequestDetailsDialog: Component<{
  target: AccessRequest | undefined;
  onClose: () => void;
}> = (props) => {
  const [detail, setDetail] = createSignal<GetAccessRequestResponse | undefined>();
  const [reason, setReason] = createSignal("");
  const [actionError, setActionError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [editing, setEditing] = createSignal(false);
  const [editDraft, setEditDraft] = createSignal<RequestDraft>({ sql: "", params: [] });

  const requestRead = createOpenFetch(
    () => getAccessRequest(props.target?.id ?? ""),
    (res) => {
      setDetail(res);
      setEditing(false);
    },
    errorMessage,
  );

  // Read when the dialog starts showing a DIFFERENT request — not when the row object changes under a refresh. The id goes through createMemo because an inline accessor re-runs the effect on every list change, which would discard the session mid-save and re-fetch over the draft being edited.
  const shownId = createMemo(() => props.target?.id);
  createEffect(
    on(shownId, (id) => {
      if (id === undefined) return;
      setDetail();
      setBusy(false);
      setActionError("");
      requestRead.handleOpenChange(true);
    }),
  );

  // Closing ends the session, so anything in flight will report "superseded" and deliberately touch nothing — which means the form's own flags have to be released HERE, or a dialog reopened during a slow request would sit disabled behind a busy() that nobody is coming back to clear.
  const handleOpenChange = (next: boolean) => {
    if (next) return;
    setBusy(false);
    setActionError("");
    requestRead.handleOpenChange(false);
    props.onClose();
  };

  // A closed dialog has no request. Its body is never rendered then, but the accessor stays total so no call site needs a non-null assertion.
  const current = () => detail()?.request ?? props.target ?? noRequest;
  const isOwner = () => {
    const s = session();
    return s.status === "authenticated" && s.user.id === current().requester?.id;
  };
  // Every affordance keys off the EFFECTIVE state via the shared rules in actions.ts, so a lazily-expired request can never show an "Expired" badge beside a live action button (ADR-0018). Approve and reject are separate permissions (a custom role may grant one without the other), so each button also carries its own key.
  const showApprove = () => mayApprove(current(), isOwner()) && hasPermission("requests.approve");
  const showReject = () => mayReject(current(), isOwner()) && hasPermission("requests.reject");
  const canDecide = () => showApprove() || showReject();
  // Check requests.create for owner actions and each decision’s own permission for reviewer actions (ADR-0008).
  const showCancel = () => mayCancel(current(), isOwner(), hasPermission);
  // The payload rides the Get response for the owner, so the edit form can prefill; without it there is nothing to edit.
  const showEditDraft = () =>
    mayEditDraft(current(), isOwner(), hasPermission) && detail()?.payload !== undefined;
  // Submitting needs no decrypted payload — the server re-reads it — so unlike Edit this stays available even when the payload did not come back.
  const showSubmitDraft = () => maySubmitDraft(current(), isOwner(), hasPermission);

  const startEditing = () => {
    const payload = detail()?.payload;
    if (!payload) return;
    setEditDraft(createRequestDraftFromPayload(payload));
    setActionError("");
    setEditing(true);
  };

  const saveEdit = async () => {
    const problem = validateRequestDraft(editDraft());
    if (problem !== "") {
      setActionError(problem);
      return;
    }
    await runAndClose(
      () =>
        updateDraft(current().id, current().version, editDraft().sql, toTypedRequestParameters(editDraft())).then(
          () => undefined,
        ),
      () => handleOpenChange(false),
    );
  };
  // Fence mutation results by dialog session; superseded responses touch no state because closing already reset it.
  const runAndClose = async (work: () => Promise<void>, close: () => void) => {
    setActionError("");
    setBusy(true);
    const outcome = await requestRead.runInSession(work);
    if (outcome.status === "superseded") return;
    setBusy(false);
    if (outcome.status === "failed") {
      setActionError(errorMessage(outcome.error));
      return;
    }
    close();
  };

  return (
    <Dialog open={props.target !== undefined} onOpenChange={handleOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle class="flex items-center gap-2">
            Request
            <Badge variant={stateBadge(current().effectiveState)}>
              {stateLabel(current().effectiveState)}
            </Badge>
          </DialogTitle>
          <DialogDescription>
            {current().connectionName} · requested by {actorLabel(current().requester)}
          </DialogDescription>
        </DialogHeader>

        <Show when={requestRead.error() === ""} fallback={<Alert variant="destructive"><AlertDescription>{requestRead.error()}</AlertDescription></Alert>}>
          <Show
            when={!editing()}
            fallback={
              // Draft edit form (owner only): change the SQL/parameters and save the SAME draft — no new request is created (§4.4).
              <form class="flex flex-col gap-4" onSubmit={(e) => e.preventDefault()}>
                <TextField>
                  <TextFieldLabel for="edit-sql">SQL</TextFieldLabel>
                  <TextFieldTextArea
                    id="edit-sql"
                    class="min-h-32 font-mono"
                    value={editDraft().sql}
                    disabled={busy()}
                    onInput={(e) => setEditDraft({ ...editDraft(), sql: e.currentTarget.value })}
                  />
                </TextField>
                <ParamEditor draft={editDraft()} onChange={setEditDraft} disabled={busy()} />
                <Show when={actionError() !== ""}>
                  <Alert variant="destructive"><AlertDescription>{actionError()}</AlertDescription></Alert>
                </Show>
                <div class="flex justify-end gap-2">
                  <Button type="button" variant="outline" disabled={busy()} onClick={() => setEditing(false)}>
                    Cancel edit
                  </Button>
                  <Button type="button" disabled={busy()} onClick={() => void saveEdit()}>
                    Save draft
                  </Button>
                </div>
              </form>
            }
          >
          <div class="flex min-w-0 flex-col gap-4">
            <Show when={detail()?.payload} fallback={
              <div class="flex flex-col gap-1">
                <span class="text-sm font-medium">SQL (redacted)</span>
                <pre class="overflow-x-auto rounded-md border border-input bg-muted p-3 text-sm">{current().redactedSql || "—"}</pre>
              </div>
            }>
              {(payload) => (
                <>
                  <div class="flex flex-col gap-1">
                    <span class="text-sm font-medium">SQL</span>
                    <pre class="overflow-x-auto rounded-md border border-input bg-muted p-3 font-mono text-sm">{payload().sql}</pre>
                  </div>
                  <Show when={payload().params.length > 0}>
                    <div class="flex flex-col gap-1">
                      <span class="text-sm font-medium">Parameters</span>
                      <For each={payload().params}>
                        {(param) => (
                          <div class="text-sm text-muted-foreground">
                            <span class="font-mono">:{param.name}</span> ({param.type}) ={" "}
                            {param.type === "null" ? "null" : param.value}
                          </div>
                        )}
                      </For>
                    </div>
                  </Show>
                </>
              )}
            </Show>

            {/* Show the submitted target snapshot rather than today’s mutable connection; drafts have no snapshot (PRD §4.3). */}
            <Show when={current().connectionFingerprint !== ""}>
              <div class="flex flex-col gap-1">
                <span class="text-sm font-medium">Target at submit</span>
                <div class="text-sm text-muted-foreground">
                  {current().connectionName} ({current().connectionDbType}) · config v
                  {current().connectionConfigVersion.toString()}
                </div>
                <div class="font-mono text-xs text-muted-foreground break-all">
                  {current().connectionFingerprint}
                </div>
              </div>
            </Show>

            <div class="flex flex-col gap-1">
              <span class="text-sm font-medium">
                Approvals ({current().validApprovals.toString()} / {current().requiredApprovals})
              </span>
              <Show when={current().approvals.length > 0} fallback={<span class="text-sm text-muted-foreground">No decisions yet.</span>}>
                <For each={current().approvals}>
                  {(decision) => (
                    <div class="flex items-center gap-2 text-sm">
                      <Badge
                        variant={
                          decision.decision === "rejected"
                            ? "destructive"
                            : decision.valid
                              ? "default"
                              : "outline"
                        }
                      >
                        {decision.decision}
                      </Badge>
                      <span>{actorLabel(decision.approver)}</span>
                      <Show when={decision.reason !== ""}>
                        <span class="text-muted-foreground">— {decision.reason}</span>
                      </Show>
                      <Show when={decision.decision === "approved" && !decision.valid}>
                        <span class="text-xs text-muted-foreground">(no longer counts)</span>
                      </Show>
                    </div>
                  )}
                </For>
              </Show>
            </div>

            <Show when={canDecide()}>
              <TextField>
                <TextFieldLabel for="decision-reason">Reason (required to reject)</TextFieldLabel>
                {/* Clamped in code points, not with maxLength: the DOM counts UTF-16 code units, so maxLength would stop the user at half the server's allowance for emoji (see entities/request/reason). */}
                <TextFieldTextArea
                  id="decision-reason"
                  value={reason()}
                  disabled={busy()}
                  onInput={(e) =>
                    setReason(truncateReasonCodePoints(e.currentTarget.value, loginConfig()?.maxApprovalReasonChars))
                  }
                />
                <Show when={loginConfig()?.maxApprovalReasonChars !== undefined}>
                  <p class="text-xs text-muted-foreground">
                    {countReasonCodePoints(reason())} / {loginConfig()?.maxApprovalReasonChars}
                  </p>
                </Show>
              </TextField>
            </Show>

            <Show when={actionError() !== ""}>
              <Alert variant="destructive"><AlertDescription>{actionError()}</AlertDescription></Alert>
            </Show>

            <div class="flex justify-end gap-2">
              <Show when={showEditDraft()}>
                <Button variant="outline" disabled={busy()} onClick={startEditing}>
                  Edit draft
                </Button>
              </Show>
              <Show when={showReject()}>
                <Button
                  variant="destructive"
                  disabled={busy()}
                  onClick={() => void runAndClose(() => rejectAccessRequest(current().id, reason()), () => handleOpenChange(false))}
                >
                  Reject
                </Button>
              </Show>
              <Show when={showApprove()}>
                <Button
                  disabled={busy()}
                  onClick={() => void runAndClose(() => approveAccessRequest(current().id, reason()), () => handleOpenChange(false))}
                >
                  Approve
                </Button>
              </Show>
              <Show when={showCancel()}>
                <Button
                  variant="outline"
                  disabled={busy()}
                  onClick={() => void runAndClose(() => cancelAccessRequest(current().id), () => handleOpenChange(false))}
                >
                  Cancel request
                </Button>
              </Show>
              {/* The draft's way forward. Submitting uses the version the server last returned, so a draft edited elsewhere conflicts here instead of overwriting that edit. */}
              <Show when={showSubmitDraft()}>
                <Button
                  disabled={busy()}
                  onClick={() =>
                    void runAndClose(
                      () => submitAccessRequest(current().id, current().version),
                      () => handleOpenChange(false),
                    )
                  }
                >
                  Submit
                </Button>
              </Show>
            </div>
          </div>
          </Show>
        </Show>
      </DialogContent>
    </Dialog>
  );
};
