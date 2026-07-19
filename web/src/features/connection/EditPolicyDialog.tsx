import type { Component } from "solid-js";
import { For, Show, createSignal } from "solid-js";

import { Code, ConnectError } from "@connectrpc/connect";

import { errorMessage } from "@/entities/connection/store";
import type { PolicyDraft, StatementClassKey } from "@/features/connection/policyDraft";
import {
  STATEMENT_CLASSES,
  autoApproveClasses,
  fromPolicy,
  newlyEnabledClasses,
  validate,
} from "@/features/connection/policyDraft";
import { createOpenFetch } from "@/features/connection/openFetch";
import { policiesClient } from "@/shared/api/client";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/shared/ui/dialog";
import { TextField, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

// EditPolicyDialog edits a connection's execution policy (ADR-0015): per-class
// allow + required approvals and one execution-limit set, saved as a NEW
// immutable version under optimistic concurrency. It is a separate dialog from
// Details on purpose — Details reads through connections.get, this reads
// policies.get, and the two permissions must stay independently gateable.
// State is dialog-local (nothing outside consumes policy state): fetch on
// open via the shared createOpenFetch guard, submit with the loaded version as
// the token.
export const EditPolicyDialog: Component<{ id: string; displayName: string }> = (props) => {
  const [loaded, setLoaded] = createSignal<PolicyDraft>();
  const [draft, setDraft] = createSignal<PolicyDraft>();
  const [saving, setSaving] = createSignal(false);

  const fetch = createOpenFetch(
    () => policiesClient.get({ connectionId: props.id }),
    (res) => {
      if (!res.policy) return;
      const d = fromPolicy(res.policy);
      setLoaded(d);
      setDraft({ ...d, read: { ...d.read }, write: { ...d.write }, ddl: { ...d.ddl } });
    },
  );

  const handleOpenChange = (next: boolean) => {
    if (next) {
      setLoaded();
      setDraft();
    }
    fetch.handleOpenChange(next);
  };

  const patchRule = (key: StatementClassKey, patch: Partial<PolicyDraft["read"]>) => {
    setDraft((d) => (d ? { ...d, [key]: { ...d[key], ...patch } } : d));
  };

  const submit = async (e: SubmitEvent) => {
    e.preventDefault();
    const d = draft();
    if (!d) return;
    const problem = validate(d);
    if (problem !== "") {
      fetch.setError(problem);
      return;
    }
    fetch.setError("");
    setSaving(true);
    try {
      await policiesClient.update({
        connectionId: props.id,
        expectedVersion: d.expectedVersion,
        read: d.read,
        write: d.write,
        ddl: d.ddl,
        queryTimeoutSeconds: d.queryTimeoutSeconds,
        maxRows: d.maxRows,
        maxResultBytes: d.maxResultBytes,
      });
      handleOpenChange(false);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Aborted) {
        // The policy moved under us. Keep the admin's DRAFT (their edits must
        // not be wiped by the other admin's values — self-review F5); refresh
        // only the concurrency token and the diff base, and tell them a
        // resubmit applies their values on top.
        const current = fetch.guard();
        try {
          const res = await policiesClient.get({ connectionId: props.id });
          if (current() && res.policy) {
            const fresh = fromPolicy(res.policy);
            setLoaded(fresh);
            setDraft((cur) => (cur ? { ...cur, expectedVersion: fresh.expectedVersion } : cur));
            fetch.setError(
              `Another admin saved this policy first (now v${fresh.expectedVersion}). Review the warnings and save again to apply your values on top.`,
            );
          }
        } catch {
          if (current()) fetch.setError(errorMessage(err));
        }
      } else {
        fetch.setError(errorMessage(err));
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={fetch.open()} onOpenChange={handleOpenChange}>
      <DialogTrigger as={Button} size="sm" variant="outline">
        Policy
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Execution policy — “{props.displayName}”</DialogTitle>
          <DialogDescription>
            Which statement classes may run here and how many approvals each needs. Saving creates a
            new policy version; requests pin the version they were approved under.
          </DialogDescription>
        </DialogHeader>
        <Show when={fetch.loading()}>
          <p class="text-sm text-muted-foreground">Loading policy…</p>
        </Show>
        <Show when={draft()}>
          {(d) => (
            <form class="flex flex-col gap-4" noValidate onSubmit={submit}>
              {/* noValidate: the numeric min/max attributes are hints only — the
                  browser's native constraint UI would otherwise swallow the
                  submit and our validate() messages (the tested contract,
                  mirroring the server bounds) would never render. */}
              <div class="flex flex-col gap-3">
                <For each={STATEMENT_CLASSES}>
                  {(cls) => (
                    <div class="flex items-end justify-between gap-4">
                      <label class="flex items-center gap-2 text-sm font-medium leading-none">
                        <input
                          type="checkbox"
                          checked={d()[cls.key].allowed}
                          onChange={(e) => patchRule(cls.key, { allowed: e.currentTarget.checked })}
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
                          value={d()[cls.key].requiredApprovals}
                          onInput={(e) =>
                            patchRule(cls.key, { requiredApprovals: e.currentTarget.valueAsNumber })
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
                    value={d().queryTimeoutSeconds}
                    onInput={(e) =>
                      setDraft((cur) =>
                        cur ? { ...cur, queryTimeoutSeconds: e.currentTarget.valueAsNumber } : cur,
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
                    value={d().maxRows}
                    onInput={(e) =>
                      setDraft((cur) => (cur ? { ...cur, maxRows: e.currentTarget.valueAsNumber } : cur))
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
                    value={Number(d().maxResultBytes / 1048576n)}
                    onInput={(e) => {
                      const mib = e.currentTarget.valueAsNumber;
                      setDraft((cur) =>
                        cur && Number.isInteger(mib)
                          ? { ...cur, maxResultBytes: BigInt(mib) * 1048576n }
                          : cur,
                      );
                    }}
                  />
                </TextField>
              </div>

              <Show when={loaded() && newlyEnabledClasses(loaded()!, d()).length > 0}>
                <Alert variant="destructive">
                  <AlertDescription>
                    Enabling {newlyEnabledClasses(loaded()!, d()).join(" and ")} allows statements
                    that can modify this database. The change is recorded in the audit trail.
                  </AlertDescription>
                </Alert>
              </Show>
              <Show when={autoApproveClasses(d()).length > 0}>
                <Alert>
                  <AlertDescription>
                    {autoApproveClasses(d()).join(", ")}: 0 approvals means requests are
                    auto-approved by the system — still fully audited.
                  </AlertDescription>
                </Alert>
              </Show>
              <Show when={fetch.error() !== ""}>
                <Alert variant="destructive">
                  <AlertDescription>{fetch.error()}</AlertDescription>
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
      </DialogContent>
    </Dialog>
  );
};
