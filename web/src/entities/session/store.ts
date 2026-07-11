import { createRoot, createSignal } from "solid-js";

import type { User } from "@/gen/portcullis/v1/auth_pb";
import { authClient } from "@/shared/api/client";

// SessionState is the SPA's view of authentication. "loading" only exists
// before the first Me round-trip resolves, so route guards can hold rendering
// instead of flashing the login page at an authenticated user.
export type SessionState =
  | { status: "loading" }
  | { status: "anonymous" }
  | { status: "authenticated"; user: User };

// Module-level store (one session per tab). createRoot detaches the signals
// from any component lifecycle.
const store = createRoot(() => {
  const [session, setSession] = createSignal<SessionState>({ status: "loading" });

  // load resolves the current session via Me. Any failure — no cookie, expired
  // session, missing CSRF — lands on "anonymous"; distinguishing causes here
  // would only re-derive what the login page already handles.
  async function load(): Promise<void> {
    try {
      const res = await authClient.me({});
      const user = res.user;
      setSession(user ? { status: "authenticated", user } : { status: "anonymous" });
    } catch {
      setSession({ status: "anonymous" });
    }
  }

  // login starts a session (cookies are set by the response) and then loads
  // the user through Me, so the store's single source of truth stays the
  // server. Errors propagate to the form, which renders ONE uniform message.
  async function login(email: string, password: string): Promise<void> {
    await authClient.login({ email, password });
    await load();
  }

  async function logout(): Promise<void> {
    try {
      await authClient.logout({});
    } finally {
      setSession({ status: "anonymous" });
    }
  }

  return { session, load, login, logout };
});

export const { session, load, login, logout } = store;
