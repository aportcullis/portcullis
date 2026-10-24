import { create, toBinary } from "@bufbuild/protobuf";
import { expect, test, type Locator, type Page, type Route } from "@playwright/test";
import assert from "node:assert/strict";

import { GetConfigResponseSchema, MeResponseSchema, type MeResponse } from "@/gen/portcullis/v1/auth_pb";
import { fulfillConnectError, fulfillEmptyMessage, type ConnectErrorCode } from "@e2e/rpc";

const logoutError = "Sign out failed — check your connection and try again.";
type LogoutReply = { kind: "success" } | { kind: "failure"; code: ConnectErrorCode } | { kind: "held" };
type CompletedLogoutReply = Exclude<LogoutReply, { kind: "held" }>;
type PendingLogout = { kind: "none" } | { kind: "captured"; route: Route };
type SessionReply = { kind: "authenticated"; snapshot: MeResponse } | { kind: "failure"; code: ConnectErrorCode };
type AccountWorkspace = {
  identity: MeResponse;
  trigger: Locator;
  panel: Locator;
  logoutCalls: () => number;
  hasPendingLogout: () => boolean;
  setLogoutReply: (reply: LogoutReply) => void;
  setSessionReply: (reply: SessionReply) => void;
  releaseLogout: (reply: CompletedLogoutReply) => Promise<void>;
};

/** Answers an intercepted logout without replacing the application's session coordinator. */
async function answerLogout(route: Route, reply: CompletedLogoutReply): Promise<void> {
  if (reply.kind === "failure") {
    await fulfillConnectError(route, reply.code);
    return;
  }
  await fulfillEmptyMessage(route);
}

/** Refreshes advisory metadata through the mounted application's normal online listener. */
async function refreshAccountSession(page: Page): Promise<void> {
  const response = page.waitForResponse("**/portcullis.v1.Auth/Me");
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await response;
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
}

/** Supplies advisory identity reads without bootstrapping or mutating the installation. */
async function openAccountWorkspace(page: Page): Promise<AccountWorkspace> {
  const identity = create(MeResponseSchema, {
    user: { id: "00000000-0000-4000-8000-000000000701", email: "operator@example.com", displayName: "Account Operator", status: "active" },
    capabilities: ["requests.create"],
    authorizationRevision: 7n,
    roleName: "Legacy role must stay hidden",
    bindings: [
      { id: "binding-organization", roleId: "role-reader", roleName: "Read reviewer", scopeType: "organization" },
      { id: "binding-connection", roleId: "role-writer", roleName: "Scoped writer", scopeType: "connection", connectionId: "00000000-0000-4000-8000-000000000702", hasCondition: true },
    ],
  });
  let sessionReply: SessionReply = { kind: "authenticated", snapshot: identity };
  let logoutReply: LogoutReply = { kind: "success" };
  let pendingLogout: PendingLogout = { kind: "none" };
  let logoutCalls = 0;
  const config = create(GetConfigResponseSchema, { needsBootstrap: false, maxRequestTitleChars: 120, maxRequestBodyChars: 1000 });
  await page.route("**/portcullis.v1.Auth/Me", route => {
    if (sessionReply.kind === "failure") return fulfillConnectError(route, sessionReply.code);
    const { snapshot } = sessionReply;
    return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(MeResponseSchema, snapshot)) });
  });
  await page.route("**/portcullis.v1.Auth/GetConfig", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(GetConfigResponseSchema, config)) }));
  await page.route("**/portcullis.v1.AccessRequests/ListRequestableConnections", fulfillEmptyMessage);
  await page.route("**/portcullis.v1.Auth/Logout", route => {
    logoutCalls++;
    if (logoutReply.kind === "held") {
      assert.equal(pendingLogout.kind, "none", "The UI must not issue a second pending Logout");
      pendingLogout = { kind: "captured", route };
      return;
    }
    return answerLogout(route, logoutReply);
  });
  await page.goto("/account-menu-fixture");
  await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Page not found", exact: true })).toBeVisible();
  return {
    identity,
    trigger: page.getByRole("button", { name: "Account menu", exact: true }),
    panel: page.getByRole("region", { name: "Account details", exact: true }),
    logoutCalls: () => logoutCalls,
    hasPendingLogout: () => pendingLogout.kind === "captured",
    setLogoutReply: reply => { logoutReply = reply; },
    setSessionReply: reply => { sessionReply = reply; },
    releaseLogout: async reply => {
      assert(pendingLogout.kind === "captured", "Expected a captured pending Logout");
      const { route } = pendingLogout;
      pendingLogout = { kind: "none" };
      const response = page.waitForResponse("**/portcullis.v1.Auth/Logout");
      await answerLogout(route, reply);
      const completed = await response;
      await completed.body();
      await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
    },
  };
}

test("avatar trigger discloses current account details without legacy roles", async ({ page }) => {
  const workspace = await openAccountWorkspace(page);
  const { trigger, panel } = workspace;
  await expect(trigger).toBeVisible();
  await expect(trigger).toHaveAttribute("aria-expanded", "false");
  await expect(trigger).toHaveAttribute("aria-haspopup", "false");
  await expect(panel).toHaveCount(0);
  await expect(page.getByText("Account Operator", { exact: true })).toHaveCount(0);
  await expect(page.getByText("operator@example.com", { exact: true })).toHaveCount(0);
  await trigger.click();
  await expect(panel).toBeVisible();
  await expect(trigger).toHaveAttribute("aria-expanded", "true");
  await expect(panel.getByText("Account Operator", { exact: true })).toBeVisible();
  await expect(panel.getByText("operator@example.com", { exact: true })).toBeVisible();
  await expect(panel.getByRole("button", { name: "Sign out", exact: true })).toBeVisible();
  await expect(panel).toContainText("Read reviewer");
  await expect(panel).toContainText(/organization/i);
  await expect(panel).toContainText("Scoped writer");
  await expect(panel).toContainText(/connection/i);
  await expect(panel).toContainText("00000000-0000-4000-8000-000000000702");
  await expect(panel).toContainText("Conditional");
  await expect(page.getByText("Legacy role must stay hidden", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("menu")).toHaveCount(0);
  await expect(page.getByRole("menuitem")).toHaveCount(0);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(workspace.logoutCalls()).toBe(0);
});

test("Enter and Space open the disclosure and Escape returns focus to its avatar", async ({ page }) => {
  const workspace = await openAccountWorkspace(page);
  const { trigger, panel } = workspace;
  for (const key of ["Enter", "Space"]) {
    await trigger.focus();
    await page.keyboard.press(key);
    await expect(panel).toBeVisible();
    await panel.getByRole("button", { name: "Sign out", exact: true }).focus();
    await page.keyboard.press("Escape");
    await expect(panel).toHaveCount(0);
    await expect(trigger).toBeFocused();
  }
  await trigger.click();
  await expect(panel).toBeVisible();
  await trigger.click();
  await expect(panel).toHaveCount(0);
  await expect(trigger).toHaveAttribute("aria-expanded", "false");
  expect(workspace.logoutCalls()).toBe(0);
});

test("native Tab and ShiftTab exit and close the panel without a focus rebound", async ({ page }) => {
  const { trigger, panel } = await openAccountWorkspace(page);
  const nextPageControl = page.getByRole("link", { name: "Go to start page", exact: true });
  await trigger.click();
  await panel.getByRole("button", { name: "Sign out", exact: true }).focus();
  await page.keyboard.press("Tab");
  await expect(panel).toHaveCount(0);
  await expect(nextPageControl).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(trigger).toBeFocused();
  await page.keyboard.press("Enter");
  await panel.getByRole("button", { name: "Sign out", exact: true }).focus();
  await page.keyboard.press("Shift+Tab");
  await expect(panel).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(nextPageControl).toBeFocused();
});

test("outside clicks retain their target focus and rapid reopen keeps the current panel usable", async ({ page }) => {
  const { trigger, panel } = await openAccountWorkspace(page);
  await trigger.click();
  const navigation = page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Requests", exact: true });
  await navigation.click();
  await expect(page).toHaveURL(/\/requests$/);
  await expect(panel).toHaveCount(0);
  await expect(navigation).toBeFocused();
  await trigger.click();
  await expect(panel).toBeVisible();
  await trigger.click();
  await trigger.click();
  await expect(panel).toBeVisible();
  const signOut = panel.getByRole("button", { name: "Sign out", exact: true });
  await signOut.focus();
  await expect(signOut).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(trigger).toBeFocused();
});

test("320px account details retain long binding summaries, email fallback and both themes", async ({ page }) => {
  const workspace = await openAccountWorkspace(page);
  workspace.setSessionReply({ kind: "authenticated", snapshot: create(MeResponseSchema, {
    user: { id: "00000000-0000-4000-8000-000000000701", email: "operator+long-account-address@example.com", displayName: "", status: "active" },
    capabilities: ["requests.create"], authorizationRevision: 8n,
    bindings: [{ id: "binding-long", roleId: "role-long", roleName: "ConnectionReviewerWithAnUnbrokenRoleNameThatMustRemainReadable", scopeType: "connection", connectionId: "00000000-0000-4000-8000-000000000702", hasCondition: true }],
  }) });
  await refreshAccountSession(page);
  await page.setViewportSize({ width: 320, height: 800 });
  for (const dark of [false, true]) {
    await page.evaluate(value => document.documentElement.classList.toggle("dark", value), dark);
    await workspace.trigger.click();
    await expect(workspace.panel).toBeVisible();
    await expect(workspace.panel.getByText("operator+long-account-address@example.com", { exact: true }).first()).toBeVisible();
    await expect(workspace.panel).toContainText("ConnectionReviewerWithAnUnbrokenRoleNameThatMustRemainReadable");
    await expect(workspace.panel).toContainText("00000000-0000-4000-8000-000000000702");
    await expect(workspace.panel.getByRole("button", { name: "Sign out", exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
    const bounds = await workspace.panel.evaluate(element => {
      const { left, right } = element.getBoundingClientRect();
      return { left, right };
    });
    expect(bounds.left).toBeGreaterThanOrEqual(0);
    expect(bounds.right).toBeLessThanOrEqual(320);
    await page.keyboard.press("Escape");
    await expect(workspace.trigger).toBeFocused();
  }
});

for (const malformedScope of ["unknown-type", "missing-connection"] satisfies ("unknown-type" | "missing-connection")[]) {
  test(`account details expose ${malformedScope} as unavailable without inventing organization scope`, async ({ page }) => {
    const workspace = await openAccountWorkspace(page);
    workspace.setSessionReply({ kind: "authenticated", snapshot: create(MeResponseSchema, {
      user: { id: "00000000-0000-4000-8000-000000000701", email: "operator@example.com", displayName: "Account Operator", status: "active" },
      capabilities: ["requests.create"], authorizationRevision: 8n,
      bindings: [{ id: "invalid-scope-binding", roleId: "invalid-scope-role", roleName: "Malformed scope reviewer", scopeType: malformedScope === "unknown-type" ? "future-scope" : "connection" }],
    }) });
    await refreshAccountSession(page);
    await workspace.trigger.click();
    await expect(workspace.panel.getByText("Scope unavailable", { exact: true })).toBeVisible();
    await expect(workspace.panel.getByText("Organization", { exact: true })).toHaveCount(0);
    expect(workspace.logoutCalls()).toBe(0);
  });
}

test("a pending logout survives close, route changes and identical metadata without a duplicate", async ({ page }) => {
  const workspace = await openAccountWorkspace(page);
  workspace.setLogoutReply({ kind: "held" });
  await workspace.trigger.click();
  await workspace.panel.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect.poll(workspace.hasPendingLogout).toBe(true);
  await expect(workspace.panel.getByRole("button", { name: "Sign out", exact: true })).toBeDisabled();
  await page.keyboard.press("Escape");
  await workspace.trigger.click();
  await expect(workspace.panel.getByRole("button", { name: "Sign out", exact: true })).toBeDisabled();
  workspace.identity.authorizationRevision++;
  await refreshAccountSession(page);
  await expect(workspace.panel.getByRole("button", { name: "Sign out", exact: true })).toBeDisabled();
  await page.keyboard.press("Escape");
  const navigation = page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Requests", exact: true });
  await navigation.click();
  await expect(page).toHaveURL(/\/requests$/);
  await expect(workspace.panel).toHaveCount(0);
  await workspace.trigger.click();
  await expect(workspace.panel.getByRole("button", { name: "Sign out", exact: true })).toBeDisabled();
  expect(workspace.logoutCalls()).toBe(1);
  await page.keyboard.press("Escape");
  await navigation.focus();
  await workspace.releaseLogout({ kind: "failure", code: "unavailable" });
  await expect(workspace.panel).toHaveCount(0);
  await expect(navigation).toBeFocused();
  await workspace.trigger.click();
  await expect(workspace.panel.getByText(logoutError, { exact: true })).toBeVisible();
  await expect(workspace.panel.getByRole("button", { name: "Sign out", exact: true })).toBeEnabled();
  workspace.identity.authorizationRevision++;
  await refreshAccountSession(page);
  await expect(workspace.panel.getByText(logoutError, { exact: true })).toBeVisible();
  workspace.setLogoutReply({ kind: "success" });
  await workspace.panel.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  expect(workspace.logoutCalls()).toBe(2);
});

test("a successful pending logout still revokes after identical metadata refresh", async ({ page }) => {
  const workspace = await openAccountWorkspace(page);
  workspace.setLogoutReply({ kind: "held" });
  await workspace.trigger.click();
  await workspace.panel.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect.poll(workspace.hasPendingLogout).toBe(true);
  workspace.identity.authorizationRevision++;
  await refreshAccountSession(page);
  await expect(workspace.panel.getByRole("button", { name: "Sign out", exact: true })).toBeDisabled();
  await workspace.releaseLogout({ kind: "success" });
  await expect(page).toHaveURL(/\/login$/);
  await expect(workspace.trigger).toHaveCount(0);
  await expect(workspace.panel).toHaveCount(0);
  expect(workspace.logoutCalls()).toBe(1);
});

for (const code of ["unauthenticated", "permission_denied"] satisfies ConnectErrorCode[]) {
  test(`logout ${code} removes account UI and reaches sign-in`, async ({ page }) => {
    const workspace = await openAccountWorkspace(page);
    workspace.setLogoutReply({ kind: "failure", code });
    await workspace.trigger.click();
    await workspace.panel.getByRole("button", { name: "Sign out", exact: true }).click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(workspace.trigger).toHaveCount(0);
    await expect(page.getByText(logoutError, { exact: true })).toHaveCount(0);
    expect(workspace.logoutCalls()).toBe(1);
  });
}

for (const outcome of ["success", "failure"] satisfies CompletedLogoutReply["kind"][]) {
  test(`identity replacement fences an old logout ${outcome}`, async ({ page }) => {
    const workspace = await openAccountWorkspace(page);
    workspace.setLogoutReply({ kind: "held" });
    await workspace.trigger.click();
    await workspace.panel.getByRole("button", { name: "Sign out", exact: true }).click();
    await expect.poll(workspace.hasPendingLogout).toBe(true);
    workspace.setSessionReply({ kind: "authenticated", snapshot: create(MeResponseSchema, {
      user: { id: "00000000-0000-4000-8000-000000000703", email: "replacement@example.com", displayName: "Replacement Operator", status: "active" },
      capabilities: ["requests.create"], authorizationRevision: 9n,
      bindings: [{ id: "replacement-binding", roleId: "replacement-role", roleName: "Replacement reviewer", scopeType: "organization" }],
    }) });
    await refreshAccountSession(page);
    await expect(workspace.panel).toHaveCount(0);
    await workspace.trigger.click();
    await expect(workspace.panel.getByText("Replacement Operator", { exact: true })).toBeVisible();
    await expect(workspace.panel.getByRole("button", { name: "Sign out", exact: true })).toBeEnabled();
    const signOut = workspace.panel.getByRole("button", { name: "Sign out", exact: true });
    await signOut.focus();
    await workspace.releaseLogout(outcome === "success" ? { kind: "success" } : { kind: "failure", code: "unavailable" });
    await expect(page).toHaveURL(/\/account-menu-fixture$/);
    await expect(workspace.panel.getByText("Replacement Operator", { exact: true })).toBeVisible();
    await expect(workspace.panel.getByText(logoutError, { exact: true })).toHaveCount(0);
    await expect(signOut).toBeEnabled();
    await expect(signOut).toBeFocused();
    expect(workspace.logoutCalls()).toBe(1);
  });
}

test("authority replacement disposes a pending account action and its late failure", async ({ page }) => {
  const workspace = await openAccountWorkspace(page);
  workspace.setLogoutReply({ kind: "held" });
  await workspace.trigger.click();
  await workspace.panel.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect.poll(workspace.hasPendingLogout).toBe(true);
  workspace.setSessionReply({ kind: "authenticated", snapshot: create(MeResponseSchema, {
    user: { id: "00000000-0000-4000-8000-000000000701", email: "operator@example.com", displayName: "Account Operator", status: "active" },
    capabilities: [], authorizationRevision: 10n, bindings: [],
  }) });
  await refreshAccountSession(page);
  await expect(workspace.panel).toHaveCount(0);
  await workspace.trigger.click();
  await expect(workspace.panel.getByText("Scoped writer", { exact: false })).toHaveCount(0);
  await expect(workspace.panel.getByRole("button", { name: "Sign out", exact: true })).toBeEnabled();
  await workspace.releaseLogout({ kind: "failure", code: "unavailable" });
  await expect(workspace.panel.getByText(logoutError, { exact: true })).toHaveCount(0);
  await expect(workspace.panel.getByRole("button", { name: "Sign out", exact: true })).toBeEnabled();
  expect(workspace.logoutCalls()).toBe(1);
});

test("session rejection removes pending account details before a delayed logout completes", async ({ page }) => {
  const workspace = await openAccountWorkspace(page);
  workspace.setLogoutReply({ kind: "held" });
  await workspace.trigger.click();
  await workspace.panel.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect.poll(workspace.hasPendingLogout).toBe(true);
  workspace.setSessionReply({ kind: "failure", code: "unauthenticated" });
  await refreshAccountSession(page);
  await expect(page).toHaveURL(/\/login\?returnTo=/);
  await expect(workspace.trigger).toHaveCount(0);
  await workspace.releaseLogout({ kind: "failure", code: "unavailable" });
  await expect(workspace.panel).toHaveCount(0);
  await expect(page.getByText(logoutError, { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Sign in", exact: true })).toBeVisible();
});
