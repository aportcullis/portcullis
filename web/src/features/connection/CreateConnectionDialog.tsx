import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import type { EnvironmentValue } from "@/entities/connection/model";
import { createConnection, errorMessage } from "@/entities/connection/store";
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

// CreateConnectionDialog is the "New connection" flow: the PG form (PRD §7.2),
// an in-form pre-save test, and the create submit. The server re-tests
// regardless — the test button is UX, the enforcement is server-side (ADR-0014).
export const CreateConnectionDialog: Component = () => {
  const [open, setOpen] = createSignal(false);
  const [displayName, setDisplayName] = createSignal("");
  const [environment, setEnvironment] = createSignal<EnvironmentValue>("development");
  const [description, setDescription] = createSignal("");
  const [error, setError] = createSignal("");
  const [saving, setSaving] = createSignal(false);
  const config = createDraftController();

  const reset = () => {
    setDisplayName("");
    setEnvironment("development");
    setDescription("");
    setError("");
    config.reset();
  };

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) reset();
  };

  const submit = async (e: SubmitEvent) => {
    e.preventDefault();
    setError("");
    setSaving(true);
    try {
      await createConnection(displayName(), environment(), description(), config.draft());
      reset();
      setOpen(false);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open()} onOpenChange={handleOpenChange}>
      <DialogTrigger as={Button}>New connection</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New connection</DialogTitle>
          <DialogDescription>
            The connection is tested before it is saved. The credential is encrypted at rest and
            never shown again.
          </DialogDescription>
        </DialogHeader>
        <form class="flex flex-col gap-4" onSubmit={submit}>
          <TextField>
            <TextFieldLabel for="conn-name">Display name</TextFieldLabel>
            <TextFieldInput
              id="conn-name"
              required
              value={displayName()}
              onInput={(e) => setDisplayName(e.currentTarget.value)}
            />
          </TextField>
          <DescriptorFields
            idPrefix="conn"
            environment={environment()}
            description={description()}
            onEnvironment={setEnvironment}
            onDescription={setDescription}
            disabled={saving()}
          />
          <ConnectionConfigForm controller={config} disabled={saving()} />
          <Show when={error() !== ""}>
            <Alert variant="destructive">
              <AlertDescription>{error()}</AlertDescription>
            </Alert>
          </Show>
          <div class="flex justify-end">
            <Button type="submit" disabled={saving()}>
              Create
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
};
