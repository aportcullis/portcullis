import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import type { EnvironmentValue } from "@/entities/connection/model";
import { parseEnvironment } from "@/entities/connection/model";
import { errorMessage, updateConnection } from "@/entities/connection/store";
import { ConnectionConfigForm } from "@/features/connection/ConnectionConfigForm";
import { DescriptorFields } from "@/features/connection/DescriptorFields";
import { createDraftController } from "@/features/connection/draft";
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

// EditConnectionDialog covers ADR-0014's two update flows: a descriptor-only
// edit (name/environment/description — no connection test), or additionally a
// FULL config replacement — the stored credential is never displayed, so
// changing anything about the target means re-entering user and password, and
// the server re-tests before persisting. An ARCHIVED connection keeps its
// descriptor editable — those fields label its history, not a live target —
// but its config is not (restore is a separate future flow).
//
// The descriptor prefills from the LIST SUMMARY via props (the summary carries
// environment and description precisely so no connections.get round-trip is
// needed here): editing must stay possible for a principal holding only
// connections.update — the list/get/update permission split is deliberate
// (ADR-0008; self-review F3).
export const EditConnectionDialog: Component<{
  id: string;
  displayName: string;
  environment: string;
  description: string;
  archived?: boolean;
}> = (props) => {
  const [open, setOpen] = createSignal(false);
  const [displayName, setDisplayName] = createSignal(props.displayName);
  const [environment, setEnvironment] = createSignal<EnvironmentValue>("development");
  const [description, setDescription] = createSignal("");
  const [replaceConfig, setReplaceConfig] = createSignal(false);
  const [error, setError] = createSignal("");
  const [saving, setSaving] = createSignal(false);
  const config = createDraftController();

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) return;
    // Reset ON OPEN from the current summary values.
    setDisplayName(props.displayName);
    setEnvironment(parseEnvironment(props.environment) ?? "development");
    setDescription(props.description);
    setReplaceConfig(false);
    setError("");
    config.reset();
  };

  const submit = async (e: SubmitEvent) => {
    e.preventDefault();
    setError("");
    setSaving(true);
    try {
      await updateConnection(
        props.id,
        displayName(),
        environment(),
        description(),
        replaceConfig() ? config.draft() : undefined,
      );
      setOpen(false);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open()} onOpenChange={handleOpenChange}>
      <DialogTrigger as={Button} size="sm" variant="outline">
        Edit
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Edit “{props.displayName}”</DialogTitle>
          <DialogDescription>
            <Show
              when={props.archived}
              fallback="Edit the name, environment, and description, or replace the full configuration. The stored credential is never shown — replacing the configuration means re-entering it, and the connection is re-tested before saving."
            >
              Edit this archived connection's name, environment, and description. They label its
              history; the configuration is no longer editable.
            </Show>
          </DialogDescription>
        </DialogHeader>
        <form class="flex flex-col gap-4" onSubmit={submit}>
          <TextField>
            <TextFieldLabel for="edit-conn-name">Display name</TextFieldLabel>
            <TextFieldInput
              id="edit-conn-name"
              required
              value={displayName()}
              onInput={(e) => setDisplayName(e.currentTarget.value)}
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
          <Show when={!props.archived}>
            <label class="flex items-center gap-2 text-sm font-medium leading-none">
              <input
                type="checkbox"
                checked={replaceConfig()}
                onChange={(e) => setReplaceConfig(e.currentTarget.checked)}
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
            {/* Only saving() disables Save: a failed submit must stay retryable
                after the user corrects the input (self-review F4). */}
            <Button type="submit" disabled={saving()}>
              Save
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
};
