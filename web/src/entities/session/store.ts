import { createRoot, createSignal } from "solid-js";

import { Code, ConnectError } from "@connectrpc/connect";

import type { User } from "@/gen/portcullis/v1/auth_pb";
import { authClient } from "@/shared/api/client";
import { retryOnThrottle } from "@/shared/api/retry";

// SessionState distinguishes pending authentication, invalid sessions, and server outages. Permissions and role labels are advisory UI data.
export type SessionState =
  | { status: "loading" }
  | { status: "anonymous" }
  | { status: "unreachable" }
  | { status: "authenticated"; user: User; permissions: readonly string[]; roleName: string };

// PermissionKey is the UI-known subset of the server's seeded permission catalog (migration 0002, ADR-0008). A union — like TlsMode and EnvironmentValue in the connection entity — so a typo'd key at a can() call site fails to compile instead of silently hiding an affordance forever. Extend as new sections (requests, audit, …) reach the UI.
export type PermissionKey =
  | "connections.list"
  | "connections.get"
  | "connections.create"
  | "connections.update"
  | "connections.test"
  | "connections.delete"
  | "policies.get"
  | "policies.update"
  | "requests.list"
  | "requests.get"
  | "requests.create"
  | "requests.approve"
  | "requests.reject"
  | "audit.list";

// Me and Logout are not permission-gated, so PermissionDenied indicates failed CSRF and invalidates the session alongside Unauthenticated.
function isAuthRejection(err: unknown): boolean {
  return (
    err instanceof ConnectError &&
    (err.code === Code.Unauthenticated || err.code === Code.PermissionDenied)
  );
}

// Module-level store (one session per tab). createRoot detaches the signals from any component lifecycle.
const store = createRoot(() => {
  const [session, setSession] = createSignal<SessionState>({ status: "loading" });

  // Apply only the latest session operation so delayed Me responses cannot overwrite a newer login or logout.
  let latest = 0;

  // load marks invalid sessions anonymous, outages unreachable, and retries transient throttling (ADR-0006/0010).
  async function load(): Promise<void> {
    const seq = ++latest;
    try {
      const res = await retryOnThrottle(() => authClient.me({}));
      const user = res.user;
      if (seq !== latest) return;
      setSession(
        user
          ? { status: "authenticated", user, permissions: res.permissions, roleName: res.roleName }
          : { status: "anonymous" },
      );
    } catch (err) {
      if (seq !== latest) return;
      setSession(isAuthRejection(err) ? { status: "anonymous" } : { status: "unreachable" });
    }
  }

  // login starts a session (cookies are set by the response) and becomes authenticated from the login response itself — no extra Me round-trip, so a transient Me failure can never turn a successful login into an apparent rejection. Errors propagate to the form, which renders ONE uniform message.
  async function login(email: string, password: string): Promise<void> {
    const seq = ++latest;
    const res = await authClient.login({ email, password });
    if (seq !== latest) return;
    const user = res.user;
    if (user) {
      setSession({ status: "authenticated", user, permissions: res.permissions, roleName: res.roleName });
      return;
    }
    // A login that succeeds without a user payload should not happen; fall back to the server as the source of truth.
    await load();
  }

  // logout goes anonymous only when the server actually revoked the session, or already considers it invalid (Unauthenticated / a CSRF PermissionDenied — the session is unusable either way). A network/server failure keeps the state and rethrows — the cookie is still alive, and pretending otherwise would misinform the user.
  async function logout(): Promise<void> {
    const seq = ++latest;
    try {
      await authClient.logout({});
    } catch (err) {
      if (!isAuthRejection(err)) {
        throw err;
      }
    }
    if (seq !== latest) return;
    setSession({ status: "anonymous" });
  }

  // can reports whether the signed-in user holds a permission key. UI affordance gating only: hiding a button is UX, the server still returns the uniform permission-denied when an RPC is attempted (ADR-0008).
  function hasPermission(permission: PermissionKey): boolean {
    const s = session();
    return s.status === "authenticated" && s.permissions.includes(permission);
  }

  return { session, load, login, logout, hasPermission };
});

export const { session, load, login, logout, hasPermission } = store;
