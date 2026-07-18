import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import { errorMessage, updateConnection } from "@/entities/connection/store";
import { ConnectionConfigForm } from "@/features/connection/ConnectionConfigForm";
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

// EditConnectionDialog covers ADR-0014's two update flows: a rename-only edit
// (no connection test), or a FULL config replacement — the stored credential
// is never displayed, so changing anything about the target means re-entering
// user and password, and the server re-tests before persisting. An ARCHIVED
// connection stays renameable — the name labels its history, not a live
// target — but its config is not editable (restore is a separate future
// flow), so the dialog collapses to rename-only.
export const EditConnectionDialog: Component<{ id: string; displayName: string; archived?: boolean }> = (props) => {
  const [open, setOpen] = createSignal(false);
  const [displayName, setDisplayName] = createSignal(props.displayName);
  const [replaceConfig, setReplaceConfig] = createSignal(false);
  const [error, setError] = createSignal("");
  const [saving, setSaving] = createSignal(false);
  const config = createDraftController();

  const reset = () => {
    setDisplayName(props.displayName);
    setReplaceConfig(false);
    setError("");
    config.reset();
  };

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (next) reset(); // reset ON OPEN so the form starts from the current name
  };

  const submit = async (e: SubmitEvent) => {
    e.preventDefault();
    setError("");
    setSaving(true);
    try {
      await updateConnection(props.id, displayName(), replaceConfig() ? config.draft() : undefined);
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
              fallback="Rename the connection, or replace its full configuration. The stored credential is never shown — replacing the configuration means re-entering it, and the connection is re-tested before saving."
            >
              Rename this archived connection. The name labels its history; the configuration is
              no longer editable.
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
            <Button type="submit" disabled={saving()}>
              Save
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
};
