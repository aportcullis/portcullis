import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import { login } from "@/entities/session/store";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { TextField, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

// ONE uniform message for every rejection — wrong password, unknown email,
// disabled account, and backoff lockout are indistinguishable by design
// (the server is oracle-free, ADR-0006; the UI must not undo that). The
// wording is deliberately gentle: it suggests the next step without blaming
// or revealing anything.
const GENERIC_ERROR = "We couldn't sign you in. Please check your email and password and try again.";

export const LoginForm: Component<{ onSuccess: () => void }> = (props) => {
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal("");
  const [pending, setPending] = createSignal(false);

  const submit = async (e: SubmitEvent) => {
    e.preventDefault();
    setError("");
    setPending(true);
    try {
      await login(email(), password());
      props.onSuccess();
    } catch {
      setError(GENERIC_ERROR);
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
        <TextFieldLabel for="password">Password</TextFieldLabel>
        <TextFieldInput
          id="password"
          type="password"
          autocomplete="current-password"
          required
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
        {pending() ? "Signing in…" : "Sign in"}
      </Button>
    </form>
  );
};
