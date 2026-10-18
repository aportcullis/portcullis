import { createRoot, createSignal } from "solid-js";

import { Code, ConnectError } from "@connectrpc/connect";

import type { SessionBinding, User } from "@/gen/portcullis/v1/auth_pb";
import { authClient } from "@/shared/api/client";
import { retryOnThrottle } from "@/shared/api/retry";

/** SessionState distinguishes invalid sessions, transport outages and unavailable authorization metadata. */
export type SessionState =
  | { status: "loading" }
  | { status: "anonymous"; cause: AnonymousCause }
  | { status: "unreachable"; cause?: "authorization" }
  | { status: "authenticated"; user: User; permissions: readonly string[]; capabilities: readonly string[]; bindings: readonly SessionBinding[]; authorizationRevision: bigint; roleName: string };

// AnonymousCause separates an explicit sign-out from finding no valid session, so only the latter offers to continue where the caller was.
export type AnonymousCause = "signed-out" | "no-session";

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
  | "requests.execute"
  | "requests.create"
  | "requests.approve"
  | "requests.reject"
  | "audit.list"
  | `users.${"list" | "get" | "create" | "update" | "disable"}`
  | `roles.${"list" | "get" | "create" | "update" | "delete"}`
  | `bindings.${"list" | "get" | "create" | "update" | "delete"}`;

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
  const [interactionGeneration, setInteractionGeneration] = createSignal(0);

  // Apply only the latest session operation so delayed Me responses cannot overwrite a newer login or logout.
  let latest = 0;

  // load marks invalid sessions anonymous, outages unreachable, and retries transient throttling (ADR-0006/0010).
  async function load(): Promise<void> {
    const seq = ++latest;
    try {
      const res = await retryOnThrottle(() => authClient.me({}));
      const user = res.user;
      if (seq !== latest) return;
      if (res.authorizationUnavailable) { setSession({ status: "unreachable", cause: "authorization" }); return; }
      setSession(
        user
          ? { status: "authenticated", user, permissions: res.permissions, capabilities: res.capabilities, bindings: res.bindings, authorizationRevision: res.authorizationRevision, roleName: res.roleName }
          : { status: "anonymous", cause: "no-session" },
      );
    } catch (err) {
      if (seq !== latest) return;
      setSession(isAuthRejection(err) ? { status: "anonymous", cause: "no-session" } : { status: "unreachable" });
    }
  }

  /** Immediately removes data owned by a refused session before resolving current admission again. */
  async function refreshRevokedSession(): Promise<void> {
    setSession({ status: "loading" });
    await load();
  }

  // login starts a session (cookies are set by the response) and becomes authenticated from the login response itself — no extra Me round-trip, so a transient Me failure can never turn a successful login into an apparent rejection. Errors propagate to the form, which renders ONE uniform message.
  async function login(email: string, password: string): Promise<void> {
    const seq = ++latest;
    const res = await authClient.login({ email, password });
    if (seq !== latest) return;
    if (res.authorizationUnavailable) { setSession({ status: "unreachable", cause: "authorization" }); return; }
    setInteractionGeneration(generation => generation + 1);
    const user = res.user;
    if (user) {
      setSession({ status: "authenticated", user, permissions: res.permissions, capabilities: res.capabilities, bindings: res.bindings, authorizationRevision: res.authorizationRevision, roleName: res.roleName });
      return;
    }
    // A login that succeeds without a user payload should not happen; fall back to the server as the source of truth.
    setSession({ status: "loading" });
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
    setSession({ status: "anonymous", cause: "signed-out" });
  }

  // can reports whether the signed-in user holds a permission key. UI affordance gating only: hiding a button is UX, the server still returns the uniform permission-denied when an RPC is attempted (ADR-0008).
  function hasPermission(permission: PermissionKey): boolean {
    const principal = session();
    return principal.status === "authenticated" && principal.capabilities.includes(permission);
  }

  /** Keeps only locally authored creation state across revision-only metadata refreshes. */
  function sessionInteractionKey(): string {
    const principal = session();
    if (principal.status !== "authenticated") return "";
    const bindingKeys = principal.bindings.map(binding => JSON.stringify([binding.id, binding.roleId, binding.roleName, binding.scopeType, binding.connectionId, binding.hasCondition])).sort();
    return JSON.stringify([interactionGeneration(), principal.user.id, principal.user.status, [...new Set(principal.permissions)].sort(), [...new Set(principal.capabilities)].sort(), principal.roleName, bindingKeys]);
  }

  return { session, load, login, logout, hasPermission, refreshRevokedSession, sessionInteractionKey };
});

export const { session, load, login, logout, hasPermission, refreshRevokedSession, sessionInteractionKey } = store;

/** Identifies the current advisory snapshot so protected views discard older responses and drafts. */
export function sessionAuthorizationKey(): string {
  const principal = session();
  return principal.status === "authenticated" ? JSON.stringify([sessionInteractionKey(), principal.authorizationRevision.toString()]) : "";
}
