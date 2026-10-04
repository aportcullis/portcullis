import { A, useNavigate } from "@solidjs/router";
import type { Component } from "solid-js";
import { For, Show, createSignal, onMount, onCleanup } from "solid-js";

import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import {
  cancelAccessRequest,
  createAccessRequest,
  getAccessRequest,
  loadAccessRequests,
  loadTargets,
  submitAccessRequest,
  targetError,
  targets,
  targetsStale,
  targetState,
  updateDraft,
} from "@/entities/request/store";
import { hasPermission } from "@/entities/session/store";
import { mayReturnToSavedDraft } from "@/features/request/actions";
import type { RequestDraft } from "@/features/request/draft";
import {
  createEmptyRequestDraft,
  resolveUnavailableTargetRequestState,
  isSelectedTargetAvailable,
  resolveUnavailableTargetAction,
  toTypedRequestParameters,
  validateRequestDraft,
} from "@/features/request/draft";
import { instanceConfig } from "@/entities/instance/config";
import { RequestNarrativeFields } from "@/features/request/RequestNarrativeFields";
import { SQLEditor } from "@/features/request/SQLEditor";
import { ParamEditor } from "@/features/request/ParamEditor";
import { errorMessage } from "@/shared/api/errors";
import type { SessionOutcome } from "@/shared/lib/dialogSession";
import { createDialogSession } from "@/shared/lib/dialogSession";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";

const selectClass =
  "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm";

/** Composes and saves a request before navigating to its stored detail page. */
export const CreateRequestForm: Component = () => {
  // Leaving the route fences responses so they cannot affect another form.
  const { discardSession, runInSession } = createDialogSession();
  const navigate = useNavigate();
  onMount(() => openList());
  onCleanup(discardSession);
  const [connectionId, setConnectionId] = createSignal("");
  const [draft, setDraft] = createSignal<RequestDraft>(createEmptyRequestDraft());
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  // Reuse the draft created in this page session on retries to avoid orphan drafts.
  const [savedId, setSavedId] = createSignal("");
  const [savedVersion, setSavedVersion] = createSignal(0n);
  // settled: the server says this draft is already finished (the archive cascade cancelled it), so the only honest affordance left is closing.
  const [settled, setSettled] = createSignal(false);

  // Refresh stale active targets through requests.create; the store exposes load failures as state.
  const openList = () => {
    if (targetState() === "idle" || targetState() === "error" || targetsStale()) {
      void refreshTargets();
    }
  };

  // Recheck target selection only after a successful refresh; network failure must not invalidate a saved draft’s fixed connection.
  const refreshTargets = async (): Promise<void> => {
    const reload = await runInSession(loadTargets);
    // Two different questions that both answer "ok": whether this page session is still the one on screen, and whether the reload itself succeeded.
    if (reload.status !== "ok") return;
    if (reload.value !== "ok") return;
    if (isSelectedTargetAvailable(targets(), connectionId())) return;
    if (resolveUnavailableTargetAction(savedId() !== "") === "repick") {
      setConnectionId("");
      setError("That connection is no longer available. Pick another one.");
      return;
    }
    // A saved draft is bound to its connection, so there is nothing to re-pick — and nothing to guess either. The archive that removed the connection also swept this draft (§4.3), so ASK the server what became of it instead of offering a cancel it would refuse.
    await reflectServerState();
  };

  // reflectServerState replaces guesswork with the row itself: settled means the work is already done (say so, refresh the list, let the user close), while a draft that somehow survived can still be withdrawn from here.
  const reflectServerState = async (): Promise<void> => {
    const id = savedId();
    if (id === "") return;
    const outcome = await runInSession(() => getAccessRequest(id));
    if (outcome.status === "superseded") return;
    if (outcome.status === "failed") {
      // The re-read failed: report that, rather than inventing a verdict.
      setError(errorMessage(outcome.error));
      return;
    }
    const state = outcome.value.request?.effectiveState ?? AccessRequestState.DRAFT;
    setSettled(resolveUnavailableTargetRequestState(state) === "already-settled");
    setError(
      settled()
        ? "This draft's connection was archived, so the draft was cancelled with it. Close this and start a new request."
        : "The connection this draft targets is no longer available, so it can no longer be submitted. Cancel the draft and start a new request.",
    );
    await runInSession(loadAccessRequests);
  };

  // discardDraft is the way out of the state above: the draft cannot be sent, so withdrawing it beats leaving a permanent dead row in the requester's list.
  const discardDraft = async (): Promise<void> => {
    const id = savedId();
    if (id === "") return;
    setBusy(true);
    const outcome = await runInSession(() => cancelAccessRequest(id));
    if (outcome.status === "superseded") return;
    setBusy(false);
    if (outcome.status === "failed") {
      setError(errorMessage(outcome.error));
      return;
    }
    reset();
    navigate("/requests");
  };

  const reset = () => {
    setConnectionId("");
    setDraft(createEmptyRequestDraft());
    setError("");
    setSavedId("");
    setSavedVersion(0n);
    setSettled(false);
  };

  // persist writes the current form to the draft — creating it the first time, updating the same draft on every subsequent call — and returns its id and current version. A saved draft, or undefined when the store fenced the response itself (a different principal signed in mid-flight) — there is then no id to submit.
  type SavedDraft = { id: string; version: bigint } | undefined;

  const persist = async (): Promise<SessionOutcome<SavedDraft>> => {
    if (savedId() === "") {
      const created = await runInSession(() =>
        createAccessRequest(connectionId(), draft().sql, toTypedRequestParameters(draft()), draft().title, draft().body),
      );
      if (created.status !== "ok") return created;
      if (!created.value) return { status: "ok", value: undefined };
      // Only NOW is this session's draft: writing the id into a form the user has since reopened would point the next request at the previous draft.
      setSavedId(created.value.id);
      setSavedVersion(created.value.version);
      return { status: "ok", value: { id: created.value.id, version: created.value.version } };
    }
    const updated = await runInSession(() =>
      updateDraft(savedId(), savedVersion(), draft().sql, toTypedRequestParameters(draft()), draft().title, draft().body),
    );
    if (updated.status !== "ok") return updated;
    if (!updated.value) return { status: "ok", value: undefined };
    setSavedVersion(updated.value.version);
    return { status: "ok", value: { id: updated.value.id, version: updated.value.version } };
  };

  // refused reports a failure on the form that asked for it. The refusal also marked the target cache stale; refresh it while the page is still open so the picker reflects the world the server just showed us, instead of waiting for a close/reopen that would discard the SQL.
  const refused = (error: unknown) => {
    setBusy(false);
    setError(errorMessage(error));
    if (targetsStale()) void refreshTargets();
  };

  const run = async (submit: boolean) => {
    if (connectionId() === "") {
      setError("Choose a connection.");
      return;
    }
    const problem = validateRequestDraft(draft(), instanceConfig());
    if (problem !== "") {
      setError(problem);
      return;
    }
    setError("");
    setBusy(true);

    const saved = await persist();
    if (saved.status === "superseded") return;
    if (saved.status === "failed") {
      refused(saved.error);
      return;
    }
    const draftRow = saved.value;
    if (submit && draftRow) {
      // Submit with the version the server actually assigned, not a hardcode.
      const sent = await runInSession(() =>
        submitAccessRequest(draftRow.id, draftRow.version),
      );
      if (sent.status === "superseded") return;
      if (sent.status === "failed") {
        refused(sent.error);
        return;
      }
    }
    setBusy(false);
    if (draftRow) navigate(hasPermission("requests.get") ? `/requests/${draftRow.id}` : "/requests");
  };

  return (
    <section aria-label="Request composition" class="flex min-w-0 flex-col gap-6">
      <A href="/requests" class="w-fit text-sm text-muted-foreground underline underline-offset-4"><span aria-hidden="true">← </span>Back to requests</A>
      <PageHeader eyebrow="Governed access" title="New access request" description="Give reviewers the context they need, then write the SQL you want approved." />
      <div class="request-workspace">
        <form class="request-form" onSubmit={(event) => event.preventDefault()}>
          <section aria-labelledby="request-context-heading" class="content-surface request-section">
            <div class="request-section-heading"><h2 id="request-context-heading">1. Request context</h2><p>Choose a database and explain the purpose of this request.</p></div>
          <div class="flex flex-col gap-1">
            <label class="text-sm font-medium" for="req-connection">
              Connection
            </label>
            <select
              id="req-connection"
              class={selectClass}
              value={connectionId()}
              // Once the draft is saved it is bound to its connection; only the payload can be edited, so the connection is locked.
              disabled={busy() || savedId() !== ""}
              onChange={(event) => setConnectionId(event.currentTarget.value)}
            >
              <option value="">
                {targetState() === "loading" ? "Loading connections…" : "Select a connection…"}
              </option>
              <For each={targets()}>
                {(target) => <option value={target.id}>{target.displayName}</option>}
              </For>
            </select>
            {/* An empty picker has two very different causes: the load failed, or this caller may target nothing. Say which, and offer a retry for the one that is retryable. */}
            <Show when={targetState() === "error"}>
              <Alert variant="destructive">
                <AlertDescription class="flex items-center justify-between gap-3">
                  <span>{targetError()}</span>
                  {/* The same path as opening the page: reload AND re-check the choice, or a target that vanished stays selected and fails again on submit. */}
                  <Button variant="outline" size="sm" onClick={() => void refreshTargets()}>
                    Retry
                  </Button>
                </AlertDescription>
              </Alert>
            </Show>
            <Show when={targetState() === "ready" && targets().length === 0}>
              <p class="text-sm text-muted-foreground">
                No connections are available to request against.
              </p>
            </Show>
          </div>
          <RequestNarrativeFields id="req" draft={draft()} disabled={busy()} onChange={setDraft} />
          </section>
          <section aria-labelledby="request-sql-heading" class="content-surface request-section">
            <div class="request-section-heading"><h2 id="request-sql-heading">2. SQL and parameters</h2><p>One statement, with exact parameter values. Review formatting before submitting.</p></div>
          <SQLEditor id="req-sql" sql={draft().sql} disabled={busy()} onChange={sql => setDraft({ ...draft(), sql })} />
          <ParamEditor draft={draft()} onChange={setDraft} disabled={busy()} />
          </section>
          <Show when={error() !== ""}>
            <Alert variant="destructive">
              <AlertDescription>{error()}</AlertDescription>
            </Alert>
          </Show>
          <div class="content-surface request-actions">
            <p class="text-xs text-muted-foreground">Submitting requests approval.<br />It does not execute SQL.</p>
            <div>
            {/* The server already cancelled this draft along with its connection (§4.3), so there is nothing left to save, send, or withdraw — only to close. Showing the other three would be showing buttons that can only fail. */}
            <Show
              when={!settled()}
              fallback={
                <Button type="button" variant="outline" onClick={() => navigate("/requests")}>
                  Close
                </Button>
              }
            >
              {/* The target is gone but the row is still a draft (not the archive cascade), so withdrawing it is a real option. */}
              <Show when={savedId() !== "" && !isSelectedTargetAvailable(targets(), connectionId())}>
                <Button
                  type="button"
                  variant="destructive"
                  disabled={busy()}
                  onClick={() => void discardDraft()}
                >
                  Cancel draft
                </Button>
              </Show>
              {/* Saving leaves a row that has to be reopened from the LIST, so a caller who cannot list requests is not offered it — they would create something they can never return to (ADR-0018). Submit stays: it hands the request on and asks nothing further. */}
              <Show when={mayReturnToSavedDraft(hasPermission)}>
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy()}
                  onClick={() => void run(false)}
                >
                  Save draft
                </Button>
              </Show>
              <Button type="button" disabled={busy()} onClick={() => void run(true)}>
                Submit
              </Button>
            </Show>
            </div>
          </div>
        </form>
        <aside class="content-surface request-guide" aria-label="Request guide">
          <h2>Before you submit</h2>
          <ol class="list-decimal pl-4">
            <li>Confirm the target database and explain the expected impact.</li>
            <li>Check the SQL and parameter values. Keep secrets out of the title and body.</li>
            <li>Save a draft to continue later, or submit for the connection's approval policy.</li>
          </ol>
          <p class="mt-4 border-t pt-4">After approval, the requester can execute once. Changes to a draft must be saved explicitly.</p>
        </aside>
      </div>
    </section>
  );
};
