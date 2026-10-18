import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LoginResponseSchema, MeResponseSchema } from "@/gen/portcullis/v1/auth_pb";

const client = vi.hoisted(() => ({ me: vi.fn(), login: vi.fn(), logout: vi.fn() }));
vi.mock("@/shared/api/client", () => ({ authClient: client }));

import { hasPermission, load, login, logout, session, sessionAuthorizationKey } from "@/entities/session/store";

/** Returns a complete current advisory view, including the valid empty-grant case. */
function currentView(capabilities = ["requests.list"], authorizationUnavailable = false) {
  return create(MeResponseSchema, { user: { id: "current", email: "current@example.com", status: "active" }, capabilities, authorizationRevision: authorizationUnavailable ? 0n : 7n, authorizationUnavailable });
}

/** Holds session responses to schedule operations in a different completion order. */
function deferred<Value>() {
  let resolve = (_value: Value): void => { throw new Error("Uninitialized deferred response"); };
  const promise = new Promise<Value>(settle => { resolve = settle; });
  return { promise, resolve };
}

beforeEach(async () => {
  vi.resetAllMocks();
  client.logout.mockResolvedValue({});
  await logout();
});

describe("current session authorization metadata", () => {
  it("shows evaluated capabilities after a successful resolve", async () => {
    client.me.mockResolvedValue(currentView());
    await load();
    expect(session().status).toBe("authenticated");
    expect(hasPermission("requests.list")).toBe(true);
  });

  it("treats a successfully resolved grantless account as authenticated", async () => {
    client.me.mockResolvedValue(currentView([]));
    await load();
    expect(session().status).toBe("authenticated");
    expect(hasPermission("requests.list")).toBe(false);
    expect(sessionAuthorizationKey()).not.toBe("");
  });

  it("removes all stale capabilities and identifies an unavailable authorization view", async () => {
    client.me.mockResolvedValueOnce(currentView()).mockResolvedValueOnce(currentView(["requests.list"], true));
    await load();
    await load();
    expect(session()).toEqual({ status: "unreachable", cause: "authorization" });
    expect(hasPermission("requests.list")).toBe(false);
    expect(sessionAuthorizationKey()).toBe("");
  });

  it("preserves a successful login as recoverable without automatically replaying credentials", async () => {
    client.login.mockResolvedValue(create(LoginResponseSchema, { user: { id: "new-user", status: "active" }, authorizationUnavailable: true }));
    await login("member@example.com", "password");
    expect(session()).toEqual({ status: "unreachable", cause: "authorization" });
    expect(client.login).toHaveBeenCalledTimes(1);
    expect(client.me).not.toHaveBeenCalled();
    expect(client.logout).toHaveBeenCalledTimes(1);
  });

  it("recovers the current view through Me without another credential write", async () => {
    client.login.mockResolvedValue(create(LoginResponseSchema, { user: { id: "current", status: "active" }, authorizationUnavailable: true }));
    await login("member@example.com", "password");
    client.me.mockResolvedValue(currentView(["connections.list"]));
    await load();
    expect(session().status).toBe("authenticated");
    expect(hasPermission("connections.list")).toBe(true);
    expect(client.login).toHaveBeenCalledTimes(1);
  });

  it("ignores an older ready response after a newer metadata failure", async () => {
    const older = deferred<ReturnType<typeof currentView>>();
    client.me.mockReturnValueOnce(older.promise).mockResolvedValueOnce(currentView([], true));
    const pending = load();
    await load();
    older.resolve(currentView());
    await pending;
    expect(session()).toEqual({ status: "unreachable", cause: "authorization" });
  });

  it("ignores a late metadata failure after a newer successful login", async () => {
    const older = deferred<ReturnType<typeof currentView>>();
    client.me.mockReturnValueOnce(older.promise);
    const pending = load();
    client.login.mockResolvedValue(create(LoginResponseSchema, { user: { id: "new-user", status: "active" }, capabilities: ["connections.list"], authorizationRevision: 9n }));
    await login("new@example.com", "password");
    older.resolve(currentView([], true));
    await pending;
    const principal = session();
    expect(principal.status).toBe("authenticated");
    if (principal.status === "authenticated") expect(principal.user.id).toBe("new-user");
    expect(hasPermission("connections.list")).toBe(true);
  });

  it.each([Code.Unauthenticated, Code.PermissionDenied])("keeps genuine session rejection distinct from metadata unavailability (%s)", async code => {
    client.me.mockRejectedValue(new ConnectError("generic refusal", code));
    await load();
    expect(session()).toEqual({ status: "anonymous", cause: "no-session" });
  });

  it("clears protected state on an ordinary transport outage", async () => {
    client.me.mockResolvedValueOnce(currentView()).mockRejectedValueOnce(new ConnectError("unavailable", Code.Unavailable));
    await load();
    await load();
    expect(session()).toEqual({ status: "unreachable" });
    expect(sessionAuthorizationKey()).toBe("");
  });
});
