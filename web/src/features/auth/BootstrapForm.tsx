import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import { describeBootstrapError } from "@/features/auth/bootstrapError";
import { PasswordField } from "@/features/auth/PasswordField";
import { authClient } from "@/shared/api/client";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { TextField, TextFieldDescription, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

export const BootstrapForm: Component<{ onSuccess: () => void }> = (props) => {
  const [setupToken, setSetupToken] = createSignal("");
  const [email, setEmail] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal("");
  const [pending, setPending] = createSignal(false);

  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    setError("");
    setPending(true);
    try {
      await authClient.bootstrap({
        setupToken: setupToken(),
        email: email(),
        password: password(),
        displayName: displayName(),
      });
      props.onSuccess();
    } catch (err) {
      setError(describeBootstrapError(err));
    } finally {
      setPending(false);
    }
  };

  return (
    <form class="flex flex-col gap-4" onSubmit={submit}>
      <TextField>
        <TextFieldLabel for="setupToken">Setup token</TextFieldLabel>
        <TextFieldInput
          id="setupToken"
          type="text"
          autocomplete="off"
          spellcheck={false}
          required
          value={setupToken()}
          onInput={(event) => setSetupToken(event.currentTarget.value)}
        />
        <TextFieldDescription>Printed once in the server log at startup, or written to the configured setup token file.</TextFieldDescription>
      </TextField>
      <TextField>
        <TextFieldLabel for="email">Email</TextFieldLabel>
        <TextFieldInput
          id="email"
          type="email"
          autocomplete="username"
          required
          value={email()}
          onInput={(event) => setEmail(event.currentTarget.value)}
        />
      </TextField>
      <TextField>
        <TextFieldLabel for="displayName">Display name</TextFieldLabel>
        <TextFieldInput
          id="displayName"
          type="text"
          required
          value={displayName()}
          onInput={(event) => setDisplayName(event.currentTarget.value)}
        />
      </TextField>
      <PasswordField
        id="password"
        label="Password (15+ characters)"
        autocomplete="new-password"
        minlength={15}
        value={password()}
        onInput={setPassword}
      />
      <Show when={error() !== ""}>
        <Alert variant="destructive">
          <AlertDescription>{error()}</AlertDescription>
        </Alert>
      </Show>
      <Button type="submit" disabled={pending()}>
        {pending() ? "Creating…" : "Create admin account"}
      </Button>
    </form>
  );
};
