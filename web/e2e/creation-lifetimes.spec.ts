import { create, toBinary } from "@bufbuild/protobuf";
import { expect, test, type Page, type Route } from "@playwright/test";

import { MeResponseSchema } from "@/gen/portcullis/v1/auth_pb";
import { CreateUserResponseSchema, GetUserResponseSchema, IssueUserSetupLinkResponseSchema, ListRolePermissionsResponseSchema } from "@/gen/portcullis/v1/administration_pb";
import { fulfillConnectError } from "@e2e/rpc";

const actorID = "00000000-0000-4000-8000-000000000001";
const memberID = "00000000-0000-4000-8000-000000000002";
const roleID = "00000000-0000-4000-8000-000000000003";
const bindingID = "00000000-0000-4000-8000-000000000004";
const capabilities = ["users.create", "users.get", "users.update", "roles.create", "bindings.create"];

/** Refreshes the actual mounted session coordinator and waits for reactive work to settle. */
async function refreshSession(page: Page): Promise<void> {
  const response = page.waitForResponse("**/portcullis.v1.Auth/Me");
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await response;
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
}

/** Changes only binary API replies while the actual SPA owns routing and local state. */
async function currentSession(page: Page) {
  const snapshot = create(MeResponseSchema, {
    user: { id: actorID, email: "operator@example.com", status: "active" },
    capabilities, permissions: ["users.create"], authorizationRevision: 7n,
    bindings: [{ id: bindingID, roleId: roleID, roleName: "Operator", scopeType: "organization" }],
  });
  await page.route("**/portcullis.v1.Auth/Me", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(MeResponseSchema, snapshot)) }));
  return snapshot;
}

/** Displays a locally issued setup receipt without borrowing list or detail permission. */
async function createUserReceipt(page: Page): Promise<void> {
  await page.route("**/portcullis.v1.Users/Create", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(CreateUserResponseSchema, create(CreateUserResponseSchema, { user: { id: memberID }, setupLink: { token: "volatile-creation-token" } }))) }));
  await page.goto("/users/new");
  await page.getByLabel("Email", { exact: true }).fill("member@example.com");
  await page.getByLabel("Display name").fill("Member");
  await page.getByRole("button", { name: "Create user", exact: true }).click();
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveValue("volatile-creation-token");
}

test("creating another user keeps its one-time delivery across changed-revision session refreshes", async ({ page }) => {
  const snapshot = await currentSession(page);
  let writes = 0;
  page.on("request", request => { if (request.url().endsWith("/portcullis.v1.Users/Create")) writes++; });
  await createUserReceipt(page);
  snapshot.authorizationRevision++;
  await refreshSession(page);
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveValue("volatile-creation-token");
  await expect(page.getByLabel("Setup link", { exact: true })).toHaveValue(/volatile-creation-token/);
  expect(page.url()).not.toContain("volatile-creation-token");
  expect(await page.evaluate(() => JSON.stringify([Object.entries(localStorage), Object.entries(sessionStorage)]))).not.toContain("volatile-creation-token");
  expect(writes).toBe(1);
  await page.getByRole("button", { name: "Dismiss setup token" }).click();
  snapshot.authorizationRevision++;
  await refreshSession(page);
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveCount(0);
});

test("user, role and conditional Binding creation inputs survive revision-only refreshes", async ({ page }) => {
  const snapshot = await currentSession(page);
  await page.goto("/users/new");
  await page.getByLabel("Display name").fill("Unsaved member");
  snapshot.authorizationRevision++;
  await refreshSession(page);
  await expect(page.getByLabel("Display name")).toHaveValue("Unsaved member");
  await page.goto("/roles/new");
  await page.getByLabel("Role name").fill("Unsaved role");
  await page.getByLabel("Permission keys").fill("connections.get");
  snapshot.authorizationRevision++;
  snapshot.capabilities.reverse();
  await refreshSession(page);
  await expect(page.getByLabel("Role name")).toHaveValue("Unsaved role");
  await expect(page.getByLabel("Permission keys")).toHaveValue("connections.get");
  await page.goto("/bindings/new");
  await page.getByLabel("User ID", { exact: true }).fill(memberID);
  await page.getByLabel("Role ID", { exact: true }).fill(roleID);
  await page.getByLabel("Condition change").selectOption("replace");
  await page.getByLabel("Condition title").fill("Unsaved restriction");
  snapshot.authorizationRevision++;
  await refreshSession(page);
  await expect(page.getByLabel("User ID", { exact: true })).toHaveValue(memberID);
  await expect(page.getByLabel("Condition title")).toHaveValue("Unsaved restriction");
});

test("reissued setup delivery outlives a revision-owned user detail refresh", async ({ page }) => {
  const snapshot = await currentSession(page);
  let detailReads = 0;
  await page.route("**/portcullis.v1.Users/Get", route => { detailReads++; return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(GetUserResponseSchema, create(GetUserResponseSchema, { user: { user: { id: memberID, email: "member@example.com", status: "active" }, hasPassword: false } }))) }); });
  await page.route("**/portcullis.v1.Users/IssueSetupLink", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(IssueUserSetupLinkResponseSchema, create(IssueUserSetupLinkResponseSchema, { setupLink: { token: "volatile-reissue-token" } }))) }));
  await page.goto(`/users/${memberID}`);
  await page.getByRole("button", { name: "Issue setup token" }).click();
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveValue("volatile-reissue-token");
  snapshot.authorizationRevision++;
  await refreshSession(page);
  await expect.poll(() => detailReads).toBe(2);
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveValue("volatile-reissue-token");
});

test("ordinary successful session refresh keeps a locally composed draft", async ({ page }) => {
  await currentSession(page);
  await page.goto("/roles/new");
  await page.getByLabel("Role name").fill("Still composing");
  await refreshSession(page);
  await expect(page.getByLabel("Role name")).toHaveValue("Still composing");
});

test("request composition keeps authored SQL while revision changes reload protected target choices", async ({ page }) => {
  const snapshot = await currentSession(page);
  snapshot.capabilities.push("requests.create");
  let targetReads = 0;
  await page.route("**/portcullis.v1.AccessRequests/ListRequestableConnections", route => { targetReads++; return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) }); });
  await page.goto("/requests/new");
  await page.getByLabel("Title", { exact: true }).fill("Authored request");
  await page.getByLabel("SQL", { exact: true }).fill("select 314159");
  snapshot.authorizationRevision++;
  await refreshSession(page);
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Authored request");
  await expect(page.getByLabel("SQL", { exact: true })).toHaveValue(/314159/);
  await expect.poll(() => targetReads).toBe(2);
});

test("connection creation inputs survive while the revision-owned collection is disposed", async ({ page }) => {
  const snapshot = await currentSession(page);
  snapshot.capabilities.push("connections.create");
  await page.goto("/connections");
  await page.getByRole("button", { name: "New connection", exact: true }).click();
  await page.getByLabel("Display name", { exact: true }).fill("Authored connection");
  snapshot.authorizationRevision++;
  await refreshSession(page);
  await expect(page.getByLabel("Display name", { exact: true })).toHaveValue("Authored connection");
});

for (const change of ["identity", "capabilities", "permissions", "binding scope"] ) {
  test(`a changed ${change} discards local delivery even when the organization revision is unchanged`, async ({ page }) => {
    const snapshot = await currentSession(page);
    await createUserReceipt(page);
    if (change === "identity") { if (!snapshot.user) throw new Error("Missing fixture user"); snapshot.user.id = memberID; }
    else if (change === "capabilities") snapshot.capabilities = snapshot.capabilities.filter(permission => permission !== "users.update");
    else if (change === "permissions") snapshot.permissions = [];
    else for (const binding of snapshot.bindings) { binding.scopeType = "connection"; binding.connectionId = memberID; }
    await refreshSession(page);
    await expect(page.getByLabel("Setup token", { exact: true })).toHaveCount(0);
    await expect(page.getByLabel("Email", { exact: true })).toHaveValue("");
  });
}

for (const failure of ["metadata", "transport", "session"]) {
  test(`${failure} failure and recovery cannot resurrect a setup receipt or draft`, async ({ page }) => {
    const snapshot = await currentSession(page);
    await createUserReceipt(page);
    if (failure === "metadata") snapshot.authorizationUnavailable = true;
    else await page.route("**/portcullis.v1.Auth/Me", route => fulfillConnectError(route, failure === "session" ? "unauthenticated" : "unavailable"));
    await refreshSession(page);
    await expect(page.getByRole("region", { name: "One-time setup delivery" })).toHaveCount(0);
    await page.unroute("**/portcullis.v1.Auth/Me");
    await currentSession(page);
    await refreshSession(page);
    await page.goto("/users/new");
    await expect(page.getByLabel("Email", { exact: true })).toHaveValue("");
    await expect(page.getByLabel("Setup token", { exact: true })).toHaveCount(0);
  });
}

test("leaving and revisiting a creation route discards its receipt", async ({ page }) => {
  await currentSession(page);
  await createUserReceipt(page);
  await page.goto("/roles/new");
  await page.goto("/users/new");
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveCount(0);
  await expect(page.getByLabel("Email", { exact: true })).toHaveValue("");
});

test("admission refusal removes a prior setup receipt without replaying the refused operation", async ({ page }) => {
  await currentSession(page);
  await page.route("**/portcullis.v1.Users/Get", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(GetUserResponseSchema, create(GetUserResponseSchema, { user: { user: { id: memberID, status: "active" } } }))) }));
  let writes = 0;
  await page.route("**/portcullis.v1.Users/IssueSetupLink", route => {
    writes++;
    if (writes > 1) return fulfillConnectError(route, "permission_denied");
    return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(IssueUserSetupLinkResponseSchema, create(IssueUserSetupLinkResponseSchema, { setupLink: { token: "refused-interaction-token" } }))) });
  });
  await page.goto(`/users/${memberID}`);
  await page.getByRole("button", { name: "Issue setup token" }).click();
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveValue("refused-interaction-token");
  await page.getByRole("button", { name: "Issue setup token" }).click();
  await expect(page.getByRole("alert")).toContainText("Permission changed");
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveCount(0);
  await refreshSession(page);
  await expect(page.getByRole("button", { name: "Issue setup token" })).toHaveCount(0);
  expect(writes).toBe(2);
});

test("a late creation response cannot deliver into a changed principal's form", async ({ page }) => {
  const snapshot = await currentSession(page);
  let pending: Route | undefined;
  await page.route("**/portcullis.v1.Users/Create", route => { pending = route; });
  await page.goto("/users/new");
  await page.getByLabel("Email", { exact: true }).fill("member@example.com");
  await page.getByLabel("Display name").fill("Member");
  await page.getByRole("button", { name: "Create user", exact: true }).click();
  await expect.poll(() => pending !== undefined).toBe(true);
  if (!snapshot.user) throw new Error("Missing fixture user");
  snapshot.user.id = memberID;
  await refreshSession(page);
  if (!pending) throw new Error("Missing pending creation");
  await pending.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(CreateUserResponseSchema, create(CreateUserResponseSchema, { user: { id: memberID }, setupLink: { token: "obsolete-completion" } }))) });
  await expect(page.getByLabel("Email", { exact: true })).toHaveValue("");
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveCount(0);
});

test("a revision disposes protected account details while keeping only its locally issued delivery", async ({ page }) => {
  const snapshot = await currentSession(page);
  let pending: Route | undefined;
  await page.route("**/portcullis.v1.Users/Get", route => {
    if (snapshot.authorizationRevision > 7n) { pending = route; return; }
    return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(GetUserResponseSchema, create(GetUserResponseSchema, { user: { user: { id: memberID, email: "protected-old-account@example.com", status: "active" } } }))) });
  });
  await page.route("**/portcullis.v1.Users/IssueSetupLink", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(IssueUserSetupLinkResponseSchema, create(IssueUserSetupLinkResponseSchema, { setupLink: { token: "route-owned-delivery" } }))) }));
  await page.goto(`/users/${memberID}`);
  await page.getByRole("button", { name: "Issue setup token" }).click();
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveValue("route-owned-delivery");
  snapshot.authorizationRevision++;
  await refreshSession(page);
  await expect.poll(() => pending !== undefined).toBe(true);
  await expect(page.getByText("protected-old-account@example.com", { exact: false })).toHaveCount(0);
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveValue("route-owned-delivery");
  if (!pending) throw new Error("Missing protected revalidation");
  await fulfillConnectError(pending, "permission_denied");
  await expect(page.getByRole("alert")).toContainText("Permission changed");
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveCount(0);
});

test("a revision during condition preparation retains authored choices but prevents the subsequent grant", async ({ page }) => {
  const snapshot = await currentSession(page);
  let pending: Route | undefined;
  let writes = 0;
  await page.route("**/portcullis.v1.Bindings/ValidateCondition", route => { pending = route; });
  await page.route("**/portcullis.v1.Bindings/Create", route => { writes++; return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) }); });
  await page.goto("/bindings/new");
  await page.getByLabel("User ID", { exact: true }).fill(memberID);
  await page.getByLabel("Role ID", { exact: true }).fill(roleID);
  await page.getByLabel("Condition change").selectOption("replace");
  await page.getByLabel("Condition title").fill("Pending restriction");
  await page.getByLabel("Resource type", { exact: true }).selectOption("request");
  await page.getByRole("button", { name: "Create binding", exact: true }).click();
  await expect.poll(() => pending !== undefined).toBe(true);
  snapshot.authorizationRevision++;
  await refreshSession(page);
  if (!pending) throw new Error("Missing condition preparation");
  await pending.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) });
  await expect(page.getByRole("alert")).toContainText("Request failed");
  await expect(page.getByLabel("Condition title")).toHaveValue("Pending restriction");
  expect(writes).toBe(0);
});

test("an existence-hiding user reread refusal also discards locally issued delivery", async ({ page }) => {
  const snapshot = await currentSession(page);
  await page.route("**/portcullis.v1.Users/Get", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(GetUserResponseSchema, create(GetUserResponseSchema, { user: { user: { id: memberID, status: "active" } } }))) }));
  await page.route("**/portcullis.v1.Users/IssueSetupLink", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(IssueUserSetupLinkResponseSchema, create(IssueUserSetupLinkResponseSchema, { setupLink: { token: "existence-hidden-token" } }))) }));
  await page.goto(`/users/${memberID}`);
  await page.getByRole("button", { name: "Issue setup token" }).click();
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveValue("existence-hidden-token");
  await page.route("**/portcullis.v1.Users/Get", route => fulfillConnectError(route, "not_found"));
  snapshot.authorizationRevision++;
  await refreshSession(page);
  await expect(page.getByRole("alert")).toContainText("Resource unavailable");
  await expect(page.getByRole("region", { name: "One-time setup delivery" })).toHaveCount(0);
});

test("Role composition retains authored keys while disposing its protected catalog and delayed catalog replies", async ({ page }) => {
  const snapshot = await currentSession(page);
  snapshot.capabilities.push("roles.list");
  let pending: Route | undefined;
  let catalogReads = 0;
  await page.route("**/portcullis.v1.Roles/ListPermissions", route => {
    catalogReads++;
    if (catalogReads > 1) { pending = route; return; }
    return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(ListRolePermissionsResponseSchema, create(ListRolePermissionsResponseSchema, { permissions: ["protected.catalog.marker"] }))) });
  });
  await page.goto("/roles/new");
  await page.getByLabel("Role name").fill("Local role draft");
  await page.getByLabel("Permission keys").fill("connections.get");
  await page.getByRole("button", { name: "Load permission catalog" }).click();
  await expect(page.getByRole("list", { name: "Permission catalog" })).toContainText("protected.catalog.marker");
  snapshot.authorizationRevision++;
  await refreshSession(page);
  await expect(page.getByRole("list", { name: "Permission catalog" })).toHaveCount(0);
  await expect(page.getByLabel("Permission keys")).toHaveValue("connections.get");
  await page.getByRole("button", { name: "Load permission catalog" }).click();
  await expect.poll(() => pending !== undefined).toBe(true);
  snapshot.authorizationRevision++;
  await refreshSession(page);
  if (!pending) throw new Error("Missing catalog read");
  const completed = page.waitForResponse("**/portcullis.v1.Roles/ListPermissions");
  await pending.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(ListRolePermissionsResponseSchema, create(ListRolePermissionsResponseSchema, { permissions: ["obsolete.catalog.marker"] }))) });
  await completed;
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
  await expect(page.getByRole("list", { name: "Permission catalog" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Load permission catalog" })).toBeEnabled();
  await expect(page.getByLabel("Role name")).toHaveValue("Local role draft");
});
