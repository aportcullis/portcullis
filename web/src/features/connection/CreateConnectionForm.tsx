import type { Component } from "solid-js";
import { Show, createSignal, onCleanup } from "solid-js";

import type { EnvironmentValue } from "@/entities/connection/model";
import { createConnection } from "@/entities/connection/store";
import { errorMessage } from "@/shared/api/errors";
import { ConnectionConfigForm } from "@/features/connection/ConnectionConfigForm";
import { DescriptorFields } from "@/features/connection/DescriptorFields";
import { createDraftController } from "@/features/connection/draft";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { createDialogSession } from "@/shared/lib/dialogSession";
import { Button } from "@/shared/ui/button";
import { InlinePanel } from "@/shared/ui/InlinePanel";
import { TextField, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

// CreateConnectionForm is the "New connection" flow: the PG form (PRD §7.2), an in-form pre-save test, and the create submit. The server re-tests regardless — the test button is UX, the enforcement is server-side (ADR-0014).
export const CreateConnectionForm: Component = () => {
  // Closing mid-save must not let the answer land on the next form the user opens: this panel is reused, so a late success would wipe fields they are already typing (see shared/lib/dialogSession).
  const { discardSession, runInSession } = createDialogSession();
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
    discardSession();
    setSaving(false); // a superseded save reports nothing, so release the form here
    setOpen(next);
    if (!next) reset();
  };

  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    setError("");
    setSaving(true);
    const outcome = await runInSession(() =>
      createConnection(displayName(), environment(), description(), config.draft()),
    );
    if (outcome.status === "superseded") return;
    setSaving(false);
    if (outcome.status === "failed") {
      setError(errorMessage(outcome.error));
      return;
    }
    reset();
    setOpen(false);
  };

  onCleanup(() => discardSession());
  return (
    <>
      <Button class="self-end" onClick={() => handleOpenChange(true)}>New connection</Button>
    <InlinePanel open={open()} label="New connection" onClose={() => handleOpenChange(false)}>

        <header>
          <h2 class="text-xl font-semibold">New connection</h2>
          <p class="text-sm text-muted-foreground">
            The connection is tested before it is saved. The credential is encrypted at rest and
            never shown again.
          </p>
        </header>
        <form class="flex flex-col gap-4" onSubmit={submit}>
          <TextField>
            <TextFieldLabel for="conn-name">Display name</TextFieldLabel>
            <TextFieldInput
              id="conn-name"
              required
              value={displayName()}
              onInput={(event) => setDisplayName(event.currentTarget.value)}
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

    </InlinePanel>
    </>
  );
};
