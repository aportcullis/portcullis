import { Show, createSignal, onCleanup } from "solid-js";
import { A } from "@solidjs/router";
import { createPasswordSetupFlow, type SetupState } from "@/entities/password-setup/model";
import { passwordSetupToken } from "@/shared/lib/passwordSetupLink";
import { authClient } from "@/shared/api/client";
import { PasswordField } from "@/features/auth/PasswordField";
import { Button } from "@/shared/ui/button";

/** Captures a fragment once and keeps registration credentials in this route lifetime only. */
export function PasswordSetupForm() {
  const [password, setPassword] = createSignal(""); const [confirmation, setConfirmation] = createSignal("");
  const captureToken = () => {
    const token = passwordSetupToken(window.location.hash);
    window.history.replaceState(window.history.state, "", window.location.pathname);
    return token;
  };
  const complete = (secret: string, value: string) => authClient.completePasswordSetup({ token: secret, password: value });
  const applyState = (next: SetupState) => {
    setState(next);
    if (["succeeded", "refused", "uncertain"].includes(next.status)) { setPassword(""); setConfirmation(""); }
  };
  let flow = createPasswordSetupFlow(captureToken(), complete, applyState);
  const [state, setState] = createSignal(flow.state());
  const replaceLink = () => {
    if (window.location.pathname !== "/setup-password") return;
    flow.dispose(); setPassword(""); setConfirmation("");
    flow = createPasswordSetupFlow(captureToken(), complete, applyState);
    applyState(flow.state());
  };
  window.addEventListener("hashchange", replaceLink);
  const existingReferrer = document.querySelector<HTMLMetaElement>('meta[name="referrer"]');
  const referrer = existingReferrer ?? document.createElement("meta");
  const previousPolicy = referrer.content;
  referrer.name = "referrer"; referrer.content = "no-referrer";
  if (!existingReferrer) document.head.append(referrer);
  onCleanup(() => { window.removeEventListener("hashchange", replaceLink); flow.dispose(); setPassword(""); setConfirmation(""); if (existingReferrer) referrer.content = previousPolicy; else referrer.remove(); });
  return <div class="flex flex-col gap-4">
    <Show when={state().message}><p role={state().status === "succeeded" ? "status" : "alert"}>{state().message}</p></Show>
    <Show when={state().status === "ready" || state().status === "busy"}>
      <form class="flex flex-col gap-4" onSubmit={event => { event.preventDefault(); void flow.submit(password(), confirmation()); }}>
        <p>Choose a password of 15 to 1024 characters. You will sign in normally after setup.</p>
        <PasswordField id="initial-password" label="New password" autocomplete="new-password" value={password()} onInput={setPassword} minlength={15} />
        <PasswordField id="confirm-initial-password" label="Confirm password" autocomplete="new-password" value={confirmation()} onInput={setConfirmation} minlength={15} />
        <Button type="submit" disabled={state().status === "busy"}>{state().status === "busy" ? "Setting password…" : "Set password"}</Button>
      </form>
    </Show>
    <Show when={state().status === "uncertain"}><Button variant="outline" onClick={() => flow.allowRetry()}>Retry setup explicitly</Button></Show>
    <A href="/login" class="underline">Sign in</A>
  </div>;
}
