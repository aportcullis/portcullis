import { A } from "@solidjs/router";
import { create } from "@bufbuild/protobuf";
import type { Component } from "solid-js";
import { For, Show, createEffect, createMemo, createSignal, on, onCleanup, onMount } from "solid-js";

import type { GetAccessRequestResponse } from "@/gen/portcullis/v1/access_requests_pb";
import { AccessRequestSchema } from "@/gen/portcullis/v1/access_requests_pb";
import { instanceConfig } from "@/entities/instance/config";
import { isExecutionOutcomeState, isLiveRequestState, stateBadge, stateLabel } from "@/entities/request/model";
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
  validateRejectReason,
} from "@/features/request/actions";
import type { RequestDraft } from "@/features/request/draft";
import { createRequestDraftFromPayload, toTypedRequestParameters, validateRequestDraft } from "@/features/request/draft";
import { RequestNarrativeFields } from "@/features/request/RequestNarrativeFields";
import { SQLEditor } from "@/features/request/SQLEditor";
import { ParamEditor } from "@/features/request/ParamEditor";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Badge } from "@/shared/ui/badge";
import { LoadingSkeleton } from "@/shared/ui/LoadingSkeleton";
import { Button } from "@/shared/ui/button";
import { RequestExecutionSummary } from "@/features/request/RequestExecutionSummary";
import { RequestRowActions } from "@/features/request/RequestRowActions";
import { TextField, TextFieldLabel, TextFieldTextArea } from "@/shared/ui/text-field";

// The stand-in for "no request is open" — see current() below.
const noRequest = create(AccessRequestSchema, {});

const actorLabel = (a?: { displayName: string; email: string }): string =>
  a ? a.displayName || a.email : "—";

/** Reads and reviews one request without replacing SQL edits during refresh. */
export const RequestDetailsPanel: Component<{
  requestId: string;
}> = (props) => {
  const [detail, setDetail] = createSignal<GetAccessRequestResponse | undefined>();
  const [reason, setReason] = createSignal("");
  const [rejectReasonError, setRejectReasonError] = createSignal("");
  const [actionError, setActionError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [editing, setEditing] = createSignal(false);
  const [editDraft, setEditDraft] = createSignal<RequestDraft>({ title: "", body: "", sql: "", params: [] });

  const requestRead = createOpenFetch(
    () => getAccessRequest(props.requestId),
    (res) => {
      setDetail(res);
      setEditing(false);
      setReason("");
      setRejectReasonError("");
    },
    errorMessage,
  );

  // Read when the page starts showing a DIFFERENT request — not when the row object changes under a refresh. The id goes through createMemo because an inline accessor re-runs the effect on every list change, which would discard the session mid-save and re-fetch over the draft being edited.
  const shownId = createMemo(() => props.requestId);
  createEffect(
    on(shownId, (id) => {
      if (id === undefined) return;
      setDetail();
      setReason("");
      setRejectReasonError("");
      setEditing(false);
      setBusy(false);
      setActionError("");
      requestRead.handleOpenChange(true);
    }),
  );

  const refresh = () => requestRead.handleOpenChange(true);
  onCleanup(() => requestRead.handleOpenChange(false));
  onMount(() => {
    // A finished request never changes again, so background refreshes stop once the loaded state is terminal.
    const isStillChanging = () => detail() === undefined || isLiveRequestState(current().effectiveState);
    const refreshIfIdle = () => { if (!document.hidden && isStillChanging() && !editing() && reason() === "" && !busy() && !requestRead.loading()) refresh(); };
    const timer = setInterval(refreshIfIdle, 30_000);
    window.addEventListener("online", refreshIfIdle);
    document.addEventListener("visibilitychange", refreshIfIdle);
    onCleanup(() => {
      clearInterval(timer);
      window.removeEventListener("online", refreshIfIdle);
      document.removeEventListener("visibilitychange", refreshIfIdle);
    });
  });
  const current = () => detail()?.request ?? noRequest;
  const isOwner = () => {
    const principal = session();
    return principal.status === "authenticated" && principal.user.id === current().requester?.id;
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
    requestRead.handleOpenChange(false);
    setEditDraft(createRequestDraftFromPayload(payload));
    setActionError("");
    setEditing(true);
  };

  const saveEdit = async () => {
    const problem = validateRequestDraft(editDraft(), instanceConfig());
    if (problem !== "") {
      setActionError(problem);
      return;
    }
    await runAndRefresh(
      () =>
        updateDraft(current().id, current().version, editDraft().sql, toTypedRequestParameters(editDraft()), editDraft().title, editDraft().body).then(
          () => undefined,
        ),
      refresh,
    );
  };
  // Fence mutation results by page session; superseded responses touch no state because closing already reset it.
  const runAndRefresh = async (work: () => Promise<void>, after: () => void) => {
    setActionError("");
    setBusy(true);
    const outcome = await requestRead.runInSession(work);
    if (outcome.status === "superseded") return;
    setBusy(false);
    if (outcome.status === "failed") {
      setActionError(errorMessage(outcome.error));
      return;
    }
    after();
  };

  return (
    <section aria-label="Request details" class="flex min-w-0 flex-col gap-6">
      <header class="request-detail-header">
        <A href="/requests" class="inline-flex rounded-md border bg-card px-3 py-2 text-sm underline-offset-4 hover:underline">Back to requests</A>
        <h1 class="mt-3 flex min-w-0 flex-wrap items-center gap-2 text-2xl font-semibold">
          <span class="min-w-0 break-words">{current().title || "Untitled request"}</span>
          <Badge variant={stateBadge(current().effectiveState)}>{stateLabel(current().effectiveState)}</Badge>
        </h1>
        <p class="text-sm text-muted-foreground">{current().connectionName} · requested by {actorLabel(current().requester)}</p>
        <p class="break-all font-mono text-xs text-muted-foreground">{props.requestId}</p>
      </header>
      <Show when={requestRead.loading() && !detail()}><LoadingSkeleton label="Loading request…" /></Show>
        {/* A failed background refresh reports above the details it could not replace; only a failed first read has nothing to keep. */}
        <Show when={requestRead.error() !== ""}>
          <Alert variant="destructive">
            <AlertDescription>
              <Show when={detail()} fallback={requestRead.error()}>
                Refresh failed: {requestRead.error()} Showing the last loaded details.
              </Show>
            </AlertDescription>
          </Alert>
        </Show>
          <Show when={detail()}>
          <Show
            when={!editing()}
            fallback={
              // Draft edit form (owner only): change the SQL/parameters and save the SAME draft — no new request is created (§4.4).
              <form class="flex flex-col gap-4" onSubmit={(event) => event.preventDefault()}>
                <RequestNarrativeFields id="edit" draft={editDraft()} disabled={busy()} onChange={setEditDraft} />
                <SQLEditor id="edit-sql" sql={editDraft().sql} disabled={busy()} onChange={sql => setEditDraft({ ...editDraft(), sql })} />
                <ParamEditor draft={editDraft()} onChange={setEditDraft} disabled={busy()} />
                <Show when={actionError() !== ""}>
                  <Alert variant="destructive"><AlertDescription>{actionError()}</AlertDescription></Alert>
                </Show>
                <div class="flex justify-end gap-2">
                  <Button type="button" variant="outline" disabled={busy()} onClick={() => { setEditing(false); refresh(); }}>
                    Cancel edit
                  </Button>
                  <Button type="button" disabled={busy()} onClick={() => void saveEdit()}>
                    Save draft
                  </Button>
                </div>
              </form>
            }
          >
          <Show when={isOwner() && hasPermission("requests.get") && isExecutionOutcomeState(current().effectiveState)}>
            <RequestExecutionSummary requestId={props.requestId} />
          </Show>
          <div class="request-review-layout">
          <div class="content-surface request-evidence">
            <h2 class="text-base font-semibold">Request evidence</h2>
            <Show when={detail()?.payload} fallback={
              <div class="flex flex-col gap-1">
                <span class="text-sm font-medium">SQL (redacted)</span>
                <pre class="overflow-x-auto rounded-md border border-input bg-muted p-3 text-sm">{current().redactedSql || "—"}</pre>
              </div>
            }>
              {(payload) => (
                <>
                  <Show when={payload().body !== ""}>
                    <div class="flex flex-col gap-1">
                      <span class="text-sm font-medium">Body</span>
                      <p class="whitespace-pre-wrap break-words rounded-md border border-input p-3 text-sm">{payload().body}</p>
                    </div>
                  </Show>
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

          </div>
          <aside aria-label="Request actions" class="content-surface decision-panel">
            <div class="request-section-heading">
              <div class="page-eyebrow">Next action</div>
              <h2>Review and execution</h2>
              <p>Current state: {stateLabel(current().effectiveState)}. {current().validApprovals} / {current().requiredApprovals} valid approvals.</p>
            </div>
            <Show when={canDecide()}><p class="text-sm text-muted-foreground">Review the target and SQL before deciding. Approval does not execute the statement.</p></Show>
            <Show when={canDecide()}>
              <TextField>
                <TextFieldLabel for="decision-reason">Reason (required to reject)</TextFieldLabel>
                {/* Clamped in code points, not with maxLength: the DOM counts UTF-16 code units, so maxLength would stop the user at half the server's allowance for emoji (see entities/request/reason). */}
                <TextFieldTextArea
                  id="decision-reason"
                  value={reason()}
                  disabled={busy()}
                  aria-invalid={rejectReasonError() !== ""}
                  aria-describedby={rejectReasonError() !== "" ? "decision-reason-error" : undefined}
                  onInput={(event) => {
                    requestRead.handleOpenChange(false);
                    setRejectReasonError("");
                    setReason(truncateReasonCodePoints(event.currentTarget.value, instanceConfig()?.maxApprovalReasonChars));
                  }}
                />
                <Show when={rejectReasonError() !== ""}>
                  <p id="decision-reason-error" class="text-xs text-destructive">{rejectReasonError()}</p>
                </Show>
                <Show when={instanceConfig()?.maxApprovalReasonChars !== undefined}>
                  <p class="text-xs text-muted-foreground">
                    {countReasonCodePoints(reason())} / {instanceConfig()?.maxApprovalReasonChars}
                  </p>
                </Show>
              </TextField>
            </Show>

            <Show when={actionError() !== ""}>
              <Alert variant="destructive"><AlertDescription>{actionError()}</AlertDescription></Alert>
            </Show>

            <RequestRowActions request={current()} onChanged={refresh} executionOnly />
            <div class="decision-buttons">
              <Show when={showEditDraft()}>
                <Button variant="outline" disabled={busy()} onClick={startEditing}>
                  Edit draft
                </Button>
              </Show>
              <Show when={showApprove()}>
                <Button
                  disabled={busy()}
                  onClick={() => void runAndRefresh(() => approveAccessRequest(current().id, reason()), refresh)}
                >
                  Approve
                </Button>
              </Show>
              <Show when={showReject()}>
                <Button
                  variant="destructive"
                  disabled={busy()}
                  onClick={() => {
                    const problem = validateRejectReason(reason());
                    setRejectReasonError(problem);
                    if (problem !== "") return;
                    void runAndRefresh(() => rejectAccessRequest(current().id, reason()), refresh);
                  }}
                >
                  Reject
                </Button>
              </Show>
              <Show when={showCancel()}>
                <Button
                  variant="outline"
                  disabled={busy()}
                  onClick={() => void runAndRefresh(() => cancelAccessRequest(current().id), refresh)}
                >
                  Cancel request
                </Button>
              </Show>
              {/* The draft's way forward. Submitting uses the version the server last returned, so a draft edited elsewhere conflicts here instead of overwriting that edit. */}
              <Show when={showSubmitDraft()}>
                <Button
                  disabled={busy()}
                  onClick={() =>
                    void runAndRefresh(
                      () => submitAccessRequest(current().id, current().version),
                      refresh,
                    )
                  }
                >
                  Submit
                </Button>
              </Show>
            </div>
            <A href="/requests" class="text-sm text-muted-foreground underline underline-offset-4">Return to request list</A>
          </aside>
          </div>
          </Show>
          </Show>
    </section>
  );
};
