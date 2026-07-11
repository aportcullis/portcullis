import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import { Code, ConnectError } from "@connectrpc/connect";

import { authClient } from "@/shared/api/client";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { TextField, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

// Bootstrap is refused once any user exists (install-level state, so naming it
// is not an account oracle); other rejections surface the server's validation
// reason category without echoing input back.
function bootstrapError(err: unknown): string {
  if (err instanceof ConnectError) {
    switch (err.code) {
      case Code.FailedPrecondition:
        return "This instance is already set up.";
      case Code.InvalidArgument:
        return "Check the email, display name, and password (15+ characters).";
      case Code.ResourceExhausted:
        return "Too many attempts — wait a moment and retry.";
    }
  }
  return "Something went wrong. Please retry.";
}

export const BootstrapForm: Component<{ onSuccess: () => void }> = (props) => {
  const [email, setEmail] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal("");
  const [pending, setPending] = createSignal(false);

  const submit = async (e: SubmitEvent) => {
    e.preventDefault();
    setError("");
    setPending(true);
    try {
      await authClient.bootstrap({
        email: email(),
        password: password(),
        displayName: displayName(),
      });
      props.onSuccess();
    } catch (err) {
      setError(bootstrapError(err));
    } finally {
      setPending(false);
    }
  };

  return (
    <form class="flex flex-col gap-4" onSubmit={submit}>
      <TextField>
        <TextFieldLabel for="email">Email</TextFieldLabel>
        <TextFieldInput
          id="email"
          type="email"
          autocomplete="username"
          required
          value={email()}
          onInput={(e) => setEmail(e.currentTarget.value)}
        />
      </TextField>
      <TextField>
        <TextFieldLabel for="displayName">Display name</TextFieldLabel>
        <TextFieldInput
          id="displayName"
          type="text"
          value={displayName()}
          onInput={(e) => setDisplayName(e.currentTarget.value)}
        />
      </TextField>
      <TextField>
        <TextFieldLabel for="password">Password (15+ characters)</TextFieldLabel>
        <TextFieldInput
          id="password"
          type="password"
          autocomplete="new-password"
          required
          minlength={15}
          value={password()}
          onInput={(e) => setPassword(e.currentTarget.value)}
        />
      </TextField>
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
