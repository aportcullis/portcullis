import type { Component } from "solid-js";
import { For, Show, createEffect, createMemo, createSignal, on, onCleanup } from "solid-js";

import { Code, ConnectError } from "@connectrpc/connect";

import { errorMessage } from "@/entities/connection/store";
import type { ConnectionSummary } from "@/gen/portcullis/v1/connections_pb";
import { conflictMessage } from "@/features/connection/policyConflict";
import { requireReturnedPolicy } from "@/features/connection/policyRead";
import { rebasePolicy } from "@/features/connection/policyRebase";
import type { PolicyDraft, StatementClassKey } from "@/features/connection/policyDraft";
import {
  STATEMENT_CLASSES,
  autoApproveClasses,
  fromPolicy,
  newlyEnabledClasses,
  validate,
} from "@/features/connection/policyDraft";
import { createOpenFetch } from "@/shared/lib/openFetch";
import { policiesClient } from "@/shared/api/client";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { InlinePanel } from "@/shared/ui/InlinePanel";
import { TextField, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

// EditPolicyPanel reads policies independently of connections.get and saves a new immutable version. The list owns its draft across row refreshes.
export const EditPolicyPanel: Component<{
  target: ConnectionSummary | undefined;
  onClose: () => void;
}> = (props) => {
  // openedFrom is the policy version this form was built on. It is the base of the three-way merge on a conflict — "did I change this field?" is only answerable against it.
  const [openedFrom, setOpenedFrom] = createSignal<PolicyDraft>();
  const [draft, setDraft] = createSignal<PolicyDraft>();
  const [saving, setSaving] = createSignal(false);

  // A response without a policy is a failed read, so the panel shows its error and Retry rather than an empty body.
  const policyRead = createOpenFetch(
    async () => requireReturnedPolicy(await policiesClient.get({ connectionId: props.target?.id ?? "" })),
    (policy) => {
      const current = fromPolicy(policy);
      setOpenedFrom(current);
      setDraft({
        ...current,
        read: { ...current.read },
        write: { ...current.write },
        ddl: { ...current.ddl },
      });
    },
    errorMessage,
  );

  // Read when the panel starts editing a DIFFERENT connection — not when the row object changes under a refresh, which would discard the admin's draft. The id goes through createMemo deliberately: an inline accessor re-runs the effect on every list change, which would re-fetch over the draft.
  const editedId = createMemo(() => props.target?.id);
  createEffect(
    on(editedId, (id) => {
      if (id === undefined) return;
      setOpenedFrom();
      setDraft();
      setSaving(false);
      policyRead.handleOpenChange(true);
    }),
  );

  const handleOpenChange = (next: boolean) => {
    if (next) return;
    // Closing ends the session, so a save still in flight will report "superseded" and touch nothing — saving has to be released here or a reopened panel would sit behind a disabled Save button forever.
    setSaving(false);
    policyRead.handleOpenChange(false);
    props.onClose();
  };

  // The classes this edit would turn on, or undefined when it turns none on — the shape <Show> narrows, so the warning below needs no assertion.
  const newlyEnabled = (was: PolicyDraft | undefined, now: PolicyDraft): string[] | undefined => {
    if (!was) return undefined;
    const enabled = newlyEnabledClasses(was, now);
    return enabled.length > 0 ? enabled : undefined;
  };

  const patchRule = (key: StatementClassKey, patch: Partial<PolicyDraft["read"]>) => {
    setDraft((current) => (current ? { ...current, [key]: { ...current[key], ...patch } } : current));
  };

  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    const edited = draft();
    const base = openedFrom();
    const target = props.target;
    if (!edited || !base || !target) return;
    const problem = validate(edited);
    if (problem !== "") {
      policyRead.setError(problem);
      return;
    }
    policyRead.setError("");
    setSaving(true);
    const outcome = await policyRead.runInSession(() =>
      policiesClient.update({
        connectionId: target.id,
        expectedVersion: edited.expectedVersion,
        read: edited.read,
        write: edited.write,
        ddl: edited.ddl,
        queryTimeoutSeconds: edited.queryTimeoutSeconds,
        maxRows: edited.maxRows,
        maxResultBytes: edited.maxResultBytes,
      }),
    );
    if (outcome.status === "superseded") return;
    setSaving(false);
    if (outcome.status === "ok") {
      handleOpenChange(false);
      return;
    }
    const err = outcome.error;
    if (!(err instanceof ConnectError) || err.code !== Code.Aborted) {
      policyRead.setError(errorMessage(err));
      return;
    }
    // The policy moved under us. The admin's edits must not be wiped by the other admin's values — but re-sending this draft whole would wipe THEIRS, because an update is a full replacement (ADR-0015). So read the current version and merge.
    const refreshed = await policyRead.runInSession(async () =>
      requireReturnedPolicy(await policiesClient.get({ connectionId: target.id })),
    );
    if (refreshed.status === "superseded") return;
    if (refreshed.status === "failed") {
      // Report the refusal AND why the retry path is closed — the token in the draft is still the stale one, so "save again" would conflict again.
      policyRead.setError(conflictMessage({ status: "stale", reason: errorMessage(refreshed.error) }));
      return;
    }
    const fresh = fromPolicy(refreshed.value);
    // Three-way merge against the version this form was opened on: fields only I touched stay mine, fields only they touched come across, and where we both moved the same one theirs wins and is named in the message.
    const { merged, conflicts } = rebasePolicy(base, edited, fresh);
    setOpenedFrom(fresh);
    setDraft(merged);
    policyRead.setError(
      conflictMessage({ status: "refreshed", version: fresh.expectedVersion, conflicts }),
    );
  };

  onCleanup(() => policyRead.handleOpenChange(false));
  return (
    <InlinePanel open={props.target !== undefined} label="Execution policy" onClose={() => handleOpenChange(false)}>

        <header>
          <h2 class="text-xl font-semibold">Execution policy — “{props.target?.displayName ?? ""}”</h2>
          <p class="text-sm text-muted-foreground">
            Which statement classes may run here and how many approvals each needs. Saving creates a
            new policy version; requests pin the version they were approved under.
          </p>
        </header>
        <Show when={policyRead.loading()}>
          <p role="status" class="text-sm text-muted-foreground">Loading policy…</p>
        </Show>
        {/* Without a draft there is no form to carry the error, so a failed initial read reports here with its own retry. */}
        <Show when={!draft() && !policyRead.loading() && policyRead.error() !== ""}>
          <Alert variant="destructive">
            <AlertDescription class="flex items-center justify-between gap-3">
              <span>{policyRead.error()}</span>
              <Button type="button" variant="outline" size="sm" onClick={() => policyRead.handleOpenChange(true)}>
                Retry
              </Button>
            </AlertDescription>
          </Alert>
        </Show>
        <Show when={draft()}>
          {(form) => (
            <form class="flex flex-col gap-4" noValidate onSubmit={submit}>
              {/* noValidate: the numeric min/max attributes are hints only — the browser's native constraint UI would otherwise swallow the submit and our validate() messages (the tested contract, mirroring the server bounds) would never render. */}
              <div class="flex flex-col gap-3">
                <For each={STATEMENT_CLASSES}>
                  {(cls) => (
                    <div class="flex items-end justify-between gap-4">
                      <label class="flex items-center gap-2 text-sm font-medium leading-none">
                        <input
                          type="checkbox"
                          checked={form()[cls.key].allowed}
                          onChange={(event) => patchRule(cls.key, { allowed: event.currentTarget.checked })}
                        />
                        Allow {cls.label}
                      </label>
                      <TextField class="w-40">
                        <TextFieldLabel for={`policy-${cls.key}-approvals`}>
                          {cls.label} approvals
                        </TextFieldLabel>
                        <TextFieldInput
                          id={`policy-${cls.key}-approvals`}
                          type="number"
                          min="0"
                          max="100"
                          value={form()[cls.key].requiredApprovals}
                          onInput={(event) =>
                            patchRule(cls.key, { requiredApprovals: event.currentTarget.valueAsNumber })
                          }
                        />
                      </TextField>
                    </div>
                  )}
                </For>
              </div>

              <div class="grid grid-cols-3 gap-3">
                <TextField>
                  <TextFieldLabel for="policy-timeout">Timeout (s)</TextFieldLabel>
                  <TextFieldInput
                    id="policy-timeout"
                    type="number"
                    min="1"
                    max="300"
                    value={form().queryTimeoutSeconds}
                    onInput={(event) =>
                      setDraft((cur) =>
                        cur ? { ...cur, queryTimeoutSeconds: event.currentTarget.valueAsNumber } : cur,
                      )
                    }
                  />
                </TextField>
                <TextField>
                  <TextFieldLabel for="policy-rows">Max rows</TextFieldLabel>
                  <TextFieldInput
                    id="policy-rows"
                    type="number"
                    min="1"
                    max="10000"
                    value={form().maxRows}
                    onInput={(event) =>
                      setDraft((cur) => (cur ? { ...cur, maxRows: event.currentTarget.valueAsNumber } : cur))
                    }
                  />
                </TextField>
                <TextField>
                  <TextFieldLabel for="policy-bytes">Max result (MiB)</TextFieldLabel>
                  <TextFieldInput
                    id="policy-bytes"
                    type="number"
                    min="1"
                    max="64"
                    value={Number(form().maxResultBytes / 1048576n)}
                    onInput={(event) => {
                      const mib = event.currentTarget.valueAsNumber;
                      setDraft((cur) =>
                        cur && Number.isInteger(mib)
                          ? { ...cur, maxResultBytes: BigInt(mib) * 1048576n }
                          : cur,
                      );
                    }}
                  />
                </TextField>
              </div>

              {/* Show's callback narrows the accessor for us, so the warning reads the classes once and needs no non-null assertion. */}
              <Show when={newlyEnabled(openedFrom(), form())}>
                {(classes) => (
                  <Alert variant="destructive">
                    <AlertDescription>
                      Enabling {classes().join(" and ")} allows statements that can modify this
                      database. The change is recorded in the audit trail.
                    </AlertDescription>
                  </Alert>
                )}
              </Show>
              <Show when={autoApproveClasses(form()).length > 0}>
                <Alert>
                  <AlertDescription>
                    {autoApproveClasses(form()).join(", ")}: 0 approvals means requests are
                    auto-approved by the system — still fully audited.
                  </AlertDescription>
                </Alert>
              </Show>
              <Show when={policyRead.error() !== ""}>
                <Alert variant="destructive">
                  <AlertDescription>{policyRead.error()}</AlertDescription>
                </Alert>
              </Show>
              <div class="flex justify-end">
                <Button type="submit" disabled={saving()}>
                  Save policy
                </Button>
              </div>
            </form>
          )}
        </Show>

    </InlinePanel>
  );
};
