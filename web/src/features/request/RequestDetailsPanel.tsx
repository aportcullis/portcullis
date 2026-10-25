import { A } from "@solidjs/router";
import type { Component, JSX } from "solid-js";
import { Show, createEffect, createMemo, createSignal, on, onCleanup, onMount } from "solid-js";

import type { ValidatedAccessRequest, ValidatedAccessRequestDetails } from "@/shared/api/accessRequestValidation";
import { AccessRequestValidationError } from "@/shared/api/accessRequestValidation";
import { instanceConfig } from "@/entities/instance/config";
import {
  formatRequestActorLabel,
  isExecutionOutcomeState,
  isLiveRequestState,
  stateBadge,
  stateLabel,
} from "@/entities/request/model";
import { truncateReasonCodePoints, countReasonCodePoints } from "@/entities/request/reason";
import {
  approveAccessRequest,
  cancelAccessRequest,
  submitAccessRequest,
  getAccessRequest,
  rejectAccessRequest,
  updateDraft,
} from "@/entities/request/store";
import { hasPermission, session } from "@/entities/session/store";
import { formatRequestMutationError } from "@/features/request/errors";
import { errorMessage, isAuthorizationRefusal } from "@/shared/api/errors";
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
import {
  createEmptyRequestDraft,
  createRequestDraftFromPayload,
  toTypedRequestParameters,
  validateRequestDraft,
} from "@/features/request/draft";
import { RequestNarrativeFields } from "@/features/request/RequestNarrativeFields";
import { SQLEditor } from "@/features/request/SQLEditor";
import { ParamEditor } from "@/features/request/ParamEditor";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Badge } from "@/shared/ui/badge";
import { LoadingSkeleton } from "@/shared/ui/LoadingSkeleton";
import { Button } from "@/shared/ui/button";
import { RequestEvidence } from "@/features/request/RequestEvidence";
import { RequestExecutionSummary } from "@/features/request/RequestExecutionSummary";
import { RequestRowActions } from "@/features/request/RequestRowActions";
import { TextField, TextFieldLabel, TextFieldTextArea } from "@/shared/ui/text-field";

type RequestDetailsReadiness = { kind: "waiting" } | { kind: "ready"; details: ValidatedAccessRequestDetails };
type ApprovalReasonLimit = { kind: "unknown" } | { kind: "known"; maxChars: number };

export type RequestDetailsPanelProps = {
  requestId: string;
};

export const RequestDetailsPanel: Component<RequestDetailsPanelProps> = (props) => {
  const [detailsState, setDetailsState] = createSignal<RequestDetailsReadiness>({ kind: "waiting" });
  const [reason, setReason] = createSignal("");
  const [rejectReasonError, setRejectReasonError] = createSignal("");
  const [actionError, setActionError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [editing, setEditing] = createSignal(false);
  const [editDraft, setEditDraft] = createSignal<RequestDraft>(createEmptyRequestDraft());
  const loadedDetails = () => {
    const state = detailsState();
    if (state.kind === "waiting") return false;
    const { details } = state;
    return details;
  };
  const reasonLimit = createMemo<ApprovalReasonLimit>(() => {
    const config = instanceConfig();
    if (config === undefined) return { kind: "unknown" };
    const { maxApprovalReasonChars } = config;
    return { kind: "known", maxChars: maxApprovalReasonChars };
  });
  const knownReasonLimit = () => {
    const limit = reasonLimit();
    return limit.kind === "known" ? limit : false;
  };

  const {
    loading: requestLoading,
    error: requestError,
    handleOpenChange: changeRequestReadOpen,
    runInSession,
  } = createOpenFetch(
    () => {
      const { requestId } = props;
      return getAccessRequest(requestId);
    },
    (details) => {
      setDetailsState({ kind: "ready", details });
      setEditing(false);
      setReason("");
      setRejectReasonError("");
    },
    (error) => {
      if (isAuthorizationRefusal(error)) {
        setDetailsState({ kind: "waiting" });
        setEditing(false);
        setEditDraft(createEmptyRequestDraft());
        setReason("");
        setBusy(false);
      }
      return error instanceof AccessRequestValidationError ? error.message : errorMessage(error);
    },
  );

  const shownId = createMemo(() => props.requestId);
  createEffect(
    on(shownId, () => {
      setDetailsState({ kind: "waiting" });
      setReason("");
      setRejectReasonError("");
      setEditing(false);
      setBusy(false);
      setActionError("");
      changeRequestReadOpen(true);
    }),
  );

  const refresh = () => changeRequestReadOpen(true);
  onCleanup(() => changeRequestReadOpen(false));
  onMount(() => {
    // A finished request never changes again, so background refreshes stop once the loaded state is terminal.
    const isStillChanging = () => {
      const state = detailsState();
      if (state.kind === "waiting") return true;
      const { details: { request: { effectiveState } } } = state;
      return isLiveRequestState(effectiveState);
    };
    const refreshIfIdle = () => {
      if (
        !document.hidden &&
        isStillChanging() &&
        !editing() &&
        reason() === "" &&
        !busy() &&
        !requestLoading()
      )
        refresh();
    };
    const timer = setInterval(refreshIfIdle, 30_000);
    window.addEventListener("online", refreshIfIdle);
    document.addEventListener("visibilitychange", refreshIfIdle);
    onCleanup(() => {
      clearInterval(timer);
      window.removeEventListener("online", refreshIfIdle);
      document.removeEventListener("visibilitychange", refreshIfIdle);
    });
  });
  const isOwner = (request: ValidatedAccessRequest) => {
    const principal = session();
    if (principal.status !== "authenticated") return false;
    const { user: { id: userId } } = principal;
    const { requester: { id: requesterId } } = request;
    return userId === requesterId;
  };
  const showApprove = (request: ValidatedAccessRequest) => mayApprove(request, isOwner(request)) && hasPermission("requests.approve");
  const showReject = (request: ValidatedAccessRequest) => mayReject(request, isOwner(request)) && hasPermission("requests.reject");
  const canDecide = (request: ValidatedAccessRequest) => showApprove(request) || showReject(request);
  const showCancel = (request: ValidatedAccessRequest) => mayCancel(request, isOwner(request), hasPermission);
  const showEditDraft = (details: ValidatedAccessRequestDetails) => {
    const { request, payload } = details;
    return mayEditDraft(request, isOwner(request), hasPermission) && payload !== undefined;
  };
  const showSubmitDraft = (request: ValidatedAccessRequest) => maySubmitDraft(request, isOwner(request), hasPermission);

  const startEditing = (details: ValidatedAccessRequestDetails) => {
    const { payload } = details;
    if (payload === undefined) return;
    changeRequestReadOpen(false);
    setEditDraft(createRequestDraftFromPayload(payload));
    setActionError("");
    setEditing(true);
  };

  const saveEdit = async (request: ValidatedAccessRequest) => {
    const draft = editDraft();
    const problem = validateRequestDraft(draft, instanceConfig());
    if (problem !== "") {
      setActionError(problem);
      return;
    }
    const { id, version } = request;
    const { sql, title, body } = draft;
    const parameters = toTypedRequestParameters(draft);
    await runAndRefresh(() => updateDraft(id, version, sql, parameters, title, body));
  };
  const runAndRefresh = async (work: () => Promise<unknown>) => {
    setActionError("");
    setBusy(true);
    const outcome = await runInSession(work);
    if (outcome.status === "superseded") return;
    setBusy(false);
    if (outcome.status === "failed") {
      const { error } = outcome;
      setActionError(formatRequestMutationError(error));
      return;
    }
    refresh();
  };

  const cancelDraftEdit = () => {
    setEditing(false);
    refresh();
  };
  const updateDecisionReason: JSX.EventHandler<HTMLTextAreaElement, InputEvent> = event => {
    const { currentTarget: { value } } = event;
    const limit = reasonLimit();
    changeRequestReadOpen(false);
    setRejectReasonError("");
    setReason(limit.kind === "known" ? truncateReasonCodePoints(value, limit.maxChars) : value);
  };
  const approveRequest = (request: ValidatedAccessRequest) => {
    const { id } = request;
    const decisionReason = reason();
    return runAndRefresh(() => approveAccessRequest(id, decisionReason));
  };
  const rejectRequest = (request: ValidatedAccessRequest) => {
    const decisionReason = reason();
    const problem = validateRejectReason(decisionReason);
    setRejectReasonError(problem);
    if (problem !== "") return;
    const { id } = request;
    void runAndRefresh(() => rejectAccessRequest(id, decisionReason));
  };
  const cancelRequest = (request: ValidatedAccessRequest) => {
    const { id } = request;
    return runAndRefresh(() => cancelAccessRequest(id));
  };
  const submitDraft = (request: ValidatedAccessRequest) => {
    const { id, version } = request;
    return runAndRefresh(() => submitAccessRequest(id, version));
  };

  return (
    <section aria-label="Request details" class="flex min-w-0 flex-col gap-6">
      <header class="request-detail-header">
        <A
          href="/requests"
          class="inline-flex rounded-md border bg-card px-3 py-2 text-sm underline-offset-4 hover:underline"
        >
          Back to requests
        </A>
        <Show when={loadedDetails()} fallback={<h1 class="mt-3 text-2xl font-semibold">Request details</h1>}>
          {details => {
            const readHeaderRequest = () => {
              const { request } = details();
              return request;
            };
            return <>
              <h1 class="mt-3 flex min-w-0 flex-wrap items-center gap-2 text-2xl font-semibold">
                <span class="min-w-0 break-words">{readHeaderRequest().title === "" ? "Untitled request" : readHeaderRequest().title}</span>
                <Badge variant={stateBadge(readHeaderRequest().effectiveState)}>
                  {stateLabel(readHeaderRequest().effectiveState)}
                </Badge>
              </h1>
              <p class="text-sm text-muted-foreground">
                {readHeaderRequest().connectionName} · requested by {formatRequestActorLabel(readHeaderRequest().requester)}
              </p>
            </>;
          }}
        </Show>
        <p class="break-all font-mono text-xs text-muted-foreground">{props.requestId}</p>
      </header>
      <Show when={requestLoading() && detailsState().kind === "waiting"}>
        <LoadingSkeleton label="Loading request…" />
      </Show>
      <Show when={requestError() !== ""}>
        <Alert variant="destructive">
          <AlertDescription>
            <Show when={loadedDetails()} fallback={requestError()}>
              Refresh failed: {requestError()} Showing the last loaded details.
            </Show>
          </AlertDescription>
        </Alert>
      </Show>
      <Show when={loadedDetails()}>
        {details => {
          const readCurrentRequest = () => {
            const { request } = details();
            return request;
          };
          return (
            <Show
              when={!editing()}
              fallback={
                <form class="flex flex-col gap-4" onSubmit={(event) => event.preventDefault()}>
                  <RequestNarrativeFields
                    id="edit"
                    draft={editDraft()}
                    disabled={busy()}
                    onChange={setEditDraft}
                  />
                  <SQLEditor
                    id="edit-sql"
                    sql={editDraft().sql}
                    disabled={busy()}
                    onChange={(sql) => setEditDraft({ ...editDraft(), sql })}
                  />
                  <ParamEditor draft={editDraft()} onChange={setEditDraft} disabled={busy()} />
                  <Show when={actionError() !== ""}>
                    <Alert variant="destructive">
                      <AlertDescription>{actionError()}</AlertDescription>
                    </Alert>
                  </Show>
                  <div class="flex justify-end gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      disabled={busy()}
                      onClick={cancelDraftEdit}
                    >
                      Cancel edit
                    </Button>
                    <Button type="button" disabled={busy()} onClick={() => void saveEdit(readCurrentRequest())}>
                      Save draft
                    </Button>
                  </div>
                </form>
              }
            >
              <Show
                when={
                  isOwner(readCurrentRequest()) &&
                  hasPermission("requests.get") &&
                  isExecutionOutcomeState(readCurrentRequest().effectiveState)
                }
              >
                <RequestExecutionSummary requestId={props.requestId} />
              </Show>
              <div class="request-review-layout">
                <RequestEvidence request={readCurrentRequest()} payload={details().payload} />
                <aside aria-label="Request actions" class="content-surface decision-panel">
                  <div class="request-section-heading">
                    <div class="page-eyebrow">Next action</div>
                    <h2>Review and execution</h2>
                    <p>
                      Current state: {stateLabel(readCurrentRequest().effectiveState)}. {readCurrentRequest().validApprovals}{" "}
                      / {readCurrentRequest().requiredApprovals} valid approvals.
                    </p>
                  </div>
                  <Show when={canDecide(readCurrentRequest())}>
                    <p class="text-sm text-muted-foreground">
                      Review the target and SQL before deciding. Approval does not execute the
                      statement.
                    </p>
                  </Show>
                  <Show when={canDecide(readCurrentRequest())}>
                    <TextField>
                      <TextFieldLabel for="decision-reason">Reason (required to reject)</TextFieldLabel>
                      {/* DOM length counts UTF-16 units; the server limits Unicode code points. */}
                      <TextFieldTextArea
                        id="decision-reason"
                        value={reason()}
                        disabled={busy()}
                        aria-invalid={rejectReasonError() !== ""}
                        aria-describedby={
                          rejectReasonError() !== "" ? "decision-reason-error" : undefined
                        }
                        onInput={updateDecisionReason}
                      />
                      <Show when={rejectReasonError() !== ""}>
                        <p id="decision-reason-error" class="text-xs text-destructive-text">
                          {rejectReasonError()}
                        </p>
                      </Show>
                      <Show when={knownReasonLimit()}>
                        {limit => (
                          <p class="text-xs text-muted-foreground">
                            {countReasonCodePoints(reason())} / {limit().maxChars}
                          </p>
                        )}
                      </Show>
                    </TextField>
                  </Show>

                  <Show when={actionError() !== ""}>
                    <Alert variant="destructive">
                      <AlertDescription>{actionError()}</AlertDescription>
                    </Alert>
                  </Show>

                  <RequestRowActions request={readCurrentRequest()} scope={{ kind: "details", refresh }} />
                  <div class="decision-buttons">
                    <Show when={showEditDraft(details())}>
                      <Button size="lg" variant="outline" disabled={busy()} onClick={() => startEditing(details())}>
                        Edit draft
                      </Button>
                    </Show>
                    <Show when={showApprove(readCurrentRequest())}>
                      <Button
                        size="lg"
                        disabled={busy()}
                        onClick={() => void approveRequest(readCurrentRequest())}
                      >
                        Approve
                      </Button>
                    </Show>
                    <Show when={showReject(readCurrentRequest())}>
                      <Button
                        size="lg"
                        variant="destructive"
                        disabled={busy()}
                        onClick={() => rejectRequest(readCurrentRequest())}
                      >
                        Reject
                      </Button>
                    </Show>
                    <Show when={showCancel(readCurrentRequest())}>
                      <Button
                        size="lg"
                        variant="outline"
                        disabled={busy()}
                        onClick={() => void cancelRequest(readCurrentRequest())}
                      >
                        Cancel request
                      </Button>
                    </Show>
                    <Show when={showSubmitDraft(readCurrentRequest())}>
                      <Button
                        size="lg"
                        disabled={busy()}
                        onClick={() => void submitDraft(readCurrentRequest())}
                      >
                        Submit
                      </Button>
                    </Show>
                  </div>
                  <A
                    href="/requests"
                    class="text-sm text-muted-foreground underline underline-offset-4"
                  >
                    Return to request list
                  </A>
                </aside>
              </div>
            </Show>
          );
        }}
      </Show>
    </section>
  );
};
