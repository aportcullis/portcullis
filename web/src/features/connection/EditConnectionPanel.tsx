import type { Component } from "solid-js";
import { Show, createEffect, createMemo, createSignal, on, onCleanup } from "solid-js";

import type { EnvironmentValue } from "@/entities/connection/model";
import { parseEnvironment } from "@/entities/connection/model";
import { errorMessage, loadConnections, updateConnection } from "@/entities/connection/store";
import { ConnectionConfigForm } from "@/features/connection/ConnectionConfigForm";
import { DescriptorFields } from "@/features/connection/DescriptorFields";
import { createDraftController } from "@/features/connection/draft";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import type { ConnectionSummary } from "@/gen/portcullis/v1/connections_pb";
import { createDialogSession } from "@/shared/lib/dialogSession";
import { Button } from "@/shared/ui/button";
import { InlinePanel } from "@/shared/ui/InlinePanel";
import { TextField, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

// Descriptor edits prefill from the list without connections.get; config changes require fresh credentials and a server test. The list owns the panel so refreshes preserve its draft.
export const EditConnectionPanel: Component<{
  target: ConnectionSummary | undefined;
  onClose: () => void;
}> = (props) => {
  const { discardSession, runInSession } = createDialogSession();
  const [displayName, setDisplayName] = createSignal("");
  const [environment, setEnvironment] = createSignal<EnvironmentValue>("development");
  const [description, setDescription] = createSignal("");
  const [replaceConfig, setReplaceConfig] = createSignal(false);
  const [error, setError] = createSignal("");
  const [saving, setSaving] = createSignal(false);
  const config = createDraftController();

  const archived = () => props.target?.archivedAt !== undefined;

  // Memoize the target ID so row refreshes do not reset the draft or discard an in-flight panel session.
  const editedId = createMemo(() => props.target?.id);
  // A typed replacement password lives only while the panel is open; every close path ends with the target cleared, and unmounting discards it too.
  const discardReplacementConfig = () => {
    config.reset();
    setReplaceConfig(false);
  };
  onCleanup(discardReplacementConfig);
  createEffect(
    on(editedId, (id) => {
      if (id === undefined) {
        discardReplacementConfig();
        return;
      }
      discardSession();
      setSaving(false);
      setDisplayName(props.target?.displayName ?? "");
      setEnvironment(parseEnvironment(props.target?.environment ?? "") ?? "development");
      setDescription(props.target?.description ?? "");
      setReplaceConfig(false);
      setError("");
      config.reset();
    }),
  );

  const handleOpenChange = (next: boolean) => {
    if (next) return;
    // A save still in flight belongs to the session being left behind: it will report "superseded" and touch nothing, so saving is released here (see shared/lib/dialogSession).
    discardSession();
    setSaving(false);
    discardReplacementConfig();
    props.onClose();
  };

  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    const target = props.target;
    if (!target) return;
    setError("");
    setSaving(true);
    const outcome = await runInSession(() =>
      updateConnection(
        target.id,
        displayName(),
        environment(),
        description(),
        target.version,
        replaceConfig() ? config.draft() : undefined,
      ),
    );
    if (outcome.status === "superseded") return;
    setSaving(false);
    if (outcome.status === "failed") {
      setError(errorMessage(outcome.error));
      // A refused save may be a conflict: someone else changed this row, so the token is stale. Reload the list — this panel is not owned by a row, so the refresh reaches it as a new `target` (fresh token) while the operator's edits stay in the local signals, and saving again applies their values on top (the policy panel's F5 rationale).
      void loadConnections();
      return;
    }
    discardReplacementConfig();
    props.onClose();
  };

  onCleanup(() => discardSession());
  return (
    <InlinePanel open={props.target !== undefined} label="Edit connection" onClose={() => handleOpenChange(false)}>

        <header>
          <h2 class="text-xl font-semibold">Edit “{props.target?.displayName ?? ""}”</h2>
          <p class="text-sm text-muted-foreground">
            <Show
              when={archived()}
              fallback="Edit the name, environment, and description, or replace the full configuration. The stored credential is never shown — replacing the configuration means re-entering it, and the connection is re-tested before saving."
            >
              Edit this archived connection's name, environment, and description. They label its
              history; the configuration is no longer editable.
            </Show>
          </p>
        </header>
        <form class="flex flex-col gap-4" onSubmit={submit}>
          <TextField>
            <TextFieldLabel for="edit-conn-name">Display name</TextFieldLabel>
            <TextFieldInput
              id="edit-conn-name"
              required
              value={displayName()}
              onInput={(event) => setDisplayName(event.currentTarget.value)}
            />
          </TextField>
          <DescriptorFields
            idPrefix="edit-conn"
            environment={environment()}
            description={description()}
            onEnvironment={setEnvironment}
            onDescription={setDescription}
            disabled={saving()}
          />
          <Show when={!archived()}>
            <label class="flex items-center gap-2 text-sm font-medium leading-none">
              <input
                type="checkbox"
                checked={replaceConfig()}
                onChange={(event) => setReplaceConfig(event.currentTarget.checked)}
              />
              Replace connection config (re-enter the credential; re-tested on save)
            </label>
            <Show when={replaceConfig()}>
              <ConnectionConfigForm controller={config} disabled={saving()} />
            </Show>
          </Show>
          <Show when={error() !== ""}>
            <Alert variant="destructive">
              <AlertDescription>{error()}</AlertDescription>
            </Alert>
          </Show>
          <div class="flex justify-end">
            {/* Only saving() disables Save: a failed submit must stay retryable after the user corrects the input. */}
            <Button type="submit" disabled={saving()}>
              Save
            </Button>
          </div>
        </form>

    </InlinePanel>
  );
};
