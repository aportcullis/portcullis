import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  LoginResponseSchema,
  LogoutResponseSchema,
  MeResponseSchema,
  UserSchema,
} from "@/gen/portcullis/v1/auth_pb";
import type { LoginRequest, LoginResponse, LogoutResponse, MeResponse } from "@/gen/portcullis/v1/auth_pb";

const client = vi.hoisted(() => ({
  me: vi.fn<() => Promise<MeResponse>>(),
  login: vi.fn<(request: LoginRequest) => Promise<LoginResponse>>(),
  logout: vi.fn<() => Promise<LogoutResponse>>(),
}));
vi.mock("@/shared/api/client", () => ({ authClient: client }));

import { load, login, logout, session, sessionInteractionKey } from "@/entities/session/store";
import type { SessionState } from "@/entities/session/store";

type DeferredReadiness<Value> =
  | { status: "preparing" }
  | { status: "ready"; resolve: (value: Value) => void; reject: (reason: Error) => void };

/** Holds a typed transport response until the scenario explicitly completes it. */
function createDeferredResponse<Value>() {
  let readiness: DeferredReadiness<Value> = { status: "preparing" };
  const promise = new Promise<Value>((resolve, reject) => {
    readiness = { status: "ready", resolve, reject };
  });
  return {
    promise,
    resolve(value: Value): void {
      if (readiness.status !== "ready") throw new Error("Deferred response is not ready");
      readiness.resolve(value);
    },
    reject(reason: Error): void {
      if (readiness.status !== "ready") throw new Error("Deferred response is not ready");
      readiness.reject(reason);
    },
  };
}

/** Supplies a complete advisory view for the chosen authenticated identity. */
function createCurrentView(userID = "current-member", authorizationRevision = 7n): MeResponse {
  return create(MeResponseSchema, {
    user: create(UserSchema, { id: userID, email: `${userID}@example.com`, status: "active" }),
    capabilities: ["requests.list"],
    permissions: ["requests.list"],
    authorizationRevision,
  });
}

/** Supplies a successful credential response without a subsequent metadata read. */
function createSignedInView(userID: string): LoginResponse {
  return create(LoginResponseSchema, {
    user: create(UserSchema, { id: userID, email: `${userID}@example.com`, status: "active" }),
    capabilities: ["requests.list"],
    permissions: ["requests.list"],
    authorizationRevision: 9n,
  });
}

/** Narrows current state before inspecting authenticated identity and authority. */
function requireAuthenticatedSession(): Extract<SessionState, { status: "authenticated" }> {
  const principal = session();
  expect(principal.status).toBe("authenticated");
  if (principal.status !== "authenticated") throw new Error("Expected an authenticated session");
  return principal;
}

beforeEach(async () => {
  vi.resetAllMocks();
  client.logout.mockResolvedValue(create(LogoutResponseSchema));
  await logout();
  client.logout.mockClear();
  client.me.mockResolvedValue(createCurrentView());
  await load();
  client.me.mockClear();
});

describe("logout completion and current session reads", () => {
  it("applies successful revocation to the ready authenticated session", async () => {
    await logout();
    expect(session()).toEqual({ status: "anonymous", cause: "signed-out" });
    expect(client.logout).toHaveBeenCalledTimes(1);
  });

  it("applies successful revocation after a newer ready Me view of the same session", async () => {
    const revoked = createDeferredResponse<LogoutResponse>();
    client.logout.mockReturnValueOnce(revoked.promise);
    const pendingLogout = logout();
    client.me.mockResolvedValueOnce(createCurrentView("current-member", 8n));
    await load();
    expect(requireAuthenticatedSession().authorizationRevision).toBe(8n);
    revoked.resolve(create(LogoutResponseSchema));
    await pendingLogout;
    expect(session()).toEqual({ status: "anonymous", cause: "signed-out" });
    expect(client.logout).toHaveBeenCalledTimes(1);
  });

  it("discards a newer pending Me view after the same session is successfully revoked", async () => {
    const revoked = createDeferredResponse<LogoutResponse>();
    const refreshed = createDeferredResponse<MeResponse>();
    client.logout.mockReturnValueOnce(revoked.promise);
    const pendingLogout = logout();
    client.me.mockReturnValueOnce(refreshed.promise);
    const pendingRead = load();
    revoked.resolve(create(LogoutResponseSchema));
    await pendingLogout;
    const afterRevocation = session();
    refreshed.resolve(createCurrentView("current-member", 8n));
    await pendingRead;
    expect(afterRevocation).toEqual({ status: "anonymous", cause: "signed-out" });
    expect(session()).toEqual({ status: "anonymous", cause: "signed-out" });
    expect(client.logout).toHaveBeenCalledTimes(1);
  });

  it("allows an explicit retry after transport failure without retrying automatically", async () => {
    const unavailable = new ConnectError("network unavailable", Code.Unavailable);
    client.logout.mockRejectedValueOnce(unavailable).mockResolvedValueOnce(create(LogoutResponseSchema));
    await expect(logout()).rejects.toBe(unavailable);
    expect(requireAuthenticatedSession().user.id).toBe("current-member");
    expect(client.logout).toHaveBeenCalledTimes(1);
    await logout();
    expect(session()).toEqual({ status: "anonymous", cause: "signed-out" });
    expect(client.logout).toHaveBeenCalledTimes(2);
  });

  it("preserves the authenticated snapshot when the server cannot confirm revocation", async () => {
    const before = requireAuthenticatedSession();
    const interaction = sessionInteractionKey();
    const unavailable = new ConnectError("network unavailable", Code.Unavailable);
    client.logout.mockRejectedValueOnce(unavailable);
    await expect(logout()).rejects.toBe(unavailable);
    expect(session()).toBe(before);
    expect(sessionInteractionKey()).toBe(interaction);
    expect(client.logout).toHaveBeenCalledTimes(1);
  });

  it.each([Code.Unauthenticated, Code.PermissionDenied])("discards an unusable session after authentication or CSRF refusal (%s)", async code => {
    client.logout.mockRejectedValueOnce(new ConnectError("session unusable", code));
    await logout();
    expect(session()).toEqual({ status: "anonymous", cause: "signed-out" });
    expect(client.logout).toHaveBeenCalledTimes(1);
  });

  it.each(["current-member", "replacement-member"])("does not let an old logout clear a later credential sign-in to %s", async userID => {
    const revoked = createDeferredResponse<LogoutResponse>();
    client.logout.mockReturnValueOnce(revoked.promise);
    const pendingLogout = logout();
    client.login.mockResolvedValueOnce(createSignedInView(userID));
    await login(`${userID}@example.com`, "password");
    const signedIn = requireAuthenticatedSession();
    const interaction = sessionInteractionKey();
    revoked.resolve(create(LogoutResponseSchema));
    await pendingLogout;
    expect(session()).toBe(signedIn);
    expect(requireAuthenticatedSession().user.id).toBe(userID);
    expect(sessionInteractionKey()).toBe(interaction);
    expect(client.login).toHaveBeenCalledTimes(1);
    expect(client.logout).toHaveBeenCalledTimes(1);
  });

  it("does not clear a different identity resolved by a later Me response", async () => {
    const revoked = createDeferredResponse<LogoutResponse>();
    client.logout.mockReturnValueOnce(revoked.promise);
    const pendingLogout = logout();
    client.me.mockResolvedValueOnce(createCurrentView("replacement-member", 12n));
    await load();
    const replacement = requireAuthenticatedSession();
    revoked.resolve(create(LogoutResponseSchema));
    await pendingLogout;
    expect(session()).toBe(replacement);
    expect(requireAuthenticatedSession().user.id).toBe("replacement-member");
    expect(client.logout).toHaveBeenCalledTimes(1);
  });

  it("discards an earlier pending Me view after successful revocation", async () => {
    const refreshed = createDeferredResponse<MeResponse>();
    client.me.mockReturnValueOnce(refreshed.promise);
    const pendingRead = load();
    await logout();
    refreshed.resolve(createCurrentView());
    await pendingRead;
    expect(session()).toEqual({ status: "anonymous", cause: "signed-out" });
    expect(client.logout).toHaveBeenCalledTimes(1);
  });
});
