import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import { logout } from "@/entities/session/store";
import { Button } from "@/shared/ui/button";

// Logout revokes the server session; the reactive session store flips to anonymous and the route guard redirects — no navigation logic here. When the server cannot be reached the store keeps the session (the cookie is still alive server-side), so the failure is surfaced instead of faking a sign-out.
export const LogoutButton: Component = () => {
  const [pending, setPending] = createSignal(false);
  const [error, setError] = createSignal("");
  const click = async () => {
    setPending(true);
    setError("");
    try {
      await logout();
    } catch {
      setError("Sign out failed — check your connection and try again.");
    } finally {
      setPending(false);
    }
  };
  return (
    <div class="flex flex-col gap-2">
      <Button variant="outline" disabled={pending()} onClick={click}>
        Sign out
      </Button>
      <Show when={error() !== ""}>
        <p class="text-sm text-destructive">{error()}</p>
      </Show>
    </div>
  );
};
