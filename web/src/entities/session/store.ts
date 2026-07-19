import { createRoot, createSignal } from "solid-js";

import { Code, ConnectError } from "@connectrpc/connect";

import type { User } from "@/gen/portcullis/v1/auth_pb";
import { authClient } from "@/shared/api/client";

// SessionState is the SPA's view of authentication. "loading" only exists
// before the first Me round-trip resolves, so route guards can hold rendering
// instead of flashing the login page at an authenticated user. "unreachable"
// means the server could not answer (network/5xx) — deliberately distinct from
// "anonymous": an infra blip must not read as a logout (ADR-0006), so guarded
// pages show a retry surface instead of bouncing to the login form.
// permissions carries the caller's permission keys (Me/Login responses) for
// affordance gating only — the server authorizes every RPC regardless.
export type SessionState =
  | { status: "loading" }
  | { status: "anonymous" }
  | { status: "unreachable" }
  | { status: "authenticated"; user: User; permissions: readonly string[] };

// PermissionKey is the UI-known subset of the server's seeded permission
// catalog (migration 0002, ADR-0008). A union — like TlsMode and
// EnvironmentValue in the connection entity — so a typo'd key at a can() call
// site fails to compile instead of silently hiding an affordance forever.
// Extend as new sections (requests, audit, …) reach the UI.
export type PermissionKey =
  | "connections.list"
  | "connections.get"
  | "connections.create"
  | "connections.update"
  | "connections.test"
  | "connections.delete"
  | "policies.get"
  | "policies.update"
  | "audit.list";

// isAuthRejection distinguishes "the server rejected the session/CSRF" from
// "the server could not answer". The store only ever calls Me and Logout, and
// NEITHER is permission-gated (no requirePermission in the Auth handler), so a
// PermissionDenied on them can only come from the CSRF interceptor — a missing
// or stale CSRF cookie (e.g. after a key rotation, ADR-0003). Both that and a
// plain Unauthenticated mean "this session is no longer usable — re-authenticate",
// so both resolve to anonymous rather than trapping the SPA in "unreachable".
function isAuthRejection(err: unknown): boolean {
  return (
    err instanceof ConnectError &&
    (err.code === Code.Unauthenticated || err.code === Code.PermissionDenied)
  );
}

// Module-level store (one session per tab). createRoot detaches the signals
// from any component lifecycle.
const store = createRoot(() => {
  const [session, setSession] = createSignal<SessionState>({ status: "loading" });

  // Every state-setting async op captures a sequence at the start and only
  // applies its result if it is still the latest. Otherwise a slow startup Me
  // resolving AFTER a login (or logout) would clobber the newer state — e.g.
  // the login form is shown during the initial "loading", so the user can sign
  // in before that first Me returns, and the stale Me must not overwrite it.
  let latest = 0;

  // load resolves the current session via Me. Only a definitive server answer
  // moves to "anonymous" (no cookie, expired session, missing CSRF — all
  // Unauthenticated); infra failures land on "unreachable" so the UI offers a
  // retry instead of pretending the user signed out (ADR-0006).
  async function load(): Promise<void> {
    const seq = ++latest;
    try {
      const res = await authClient.me({});
      const user = res.user;
      if (seq !== latest) return;
      setSession(
        user
          ? { status: "authenticated", user, permissions: res.permissions }
          : { status: "anonymous" },
      );
    } catch (err) {
      if (seq !== latest) return;
      setSession(isAuthRejection(err) ? { status: "anonymous" } : { status: "unreachable" });
    }
  }

  // login starts a session (cookies are set by the response) and becomes
  // authenticated from the login response itself — no extra Me round-trip, so
  // a transient Me failure can never turn a successful login into an apparent
  // rejection. Errors propagate to the form, which renders ONE uniform message.
  async function login(email: string, password: string): Promise<void> {
    const seq = ++latest;
    const res = await authClient.login({ email, password });
    if (seq !== latest) return;
    const user = res.user;
    if (user) {
      setSession({ status: "authenticated", user, permissions: res.permissions });
      return;
    }
    // A login that succeeds without a user payload should not happen; fall
    // back to the server as the source of truth.
    await load();
  }

  // logout goes anonymous only when the server actually revoked the session,
  // or already considers it invalid (Unauthenticated / a CSRF PermissionDenied —
  // the session is unusable either way). A network/server failure keeps the
  // state and rethrows — the cookie is still alive, and pretending otherwise
  // would misinform the user (external review finding).
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

  // can reports whether the signed-in user holds a permission key. UI
  // affordance gating only: hiding a button is UX, the server still returns
  // the uniform permission-denied when an RPC is attempted (ADR-0008).
  function can(permission: PermissionKey): boolean {
    const s = session();
    return s.status === "authenticated" && s.permissions.includes(permission);
  }

  return { session, load, login, logout, can };
});

export const { session, load, login, logout, can } = store;
