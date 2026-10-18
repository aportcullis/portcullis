import { create, fromBinary, toBinary, type DescMessage, type MessageInitShape } from "@bufbuild/protobuf";
import { expect, test, type Page, type Route } from "@playwright/test";

import * as management from "@/gen/portcullis/v1/administration_pb";
import { MeResponseSchema } from "@/gen/portcullis/v1/auth_pb";
import { fulfillConnectError } from "@e2e/rpc";

const actor = "00000000-0000-4000-8000-000000000001";
const member = "00000000-0000-4000-8000-000000000002";
const roleID = "00000000-0000-4000-8000-000000000003";
const bindingID = "00000000-0000-4000-8000-000000000004";
const permissions = ["users.list", "users.get", "users.create", "users.disable", "users.update", "roles.list", "roles.get", "roles.create", "roles.update", "roles.delete", "bindings.list", "bindings.get", "bindings.create", "bindings.update", "bindings.delete"];

/** Supplies binary API contracts for UI interaction qualification, never server authorization evidence. */
async function answer<Schema extends DescMessage>(route: Route, schema: Schema, value: MessageInitShape<Schema>) {
  await route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(schema, create(schema, value))) });
}

/** Gives an isolated page advisory capabilities without changing the real installation's authority. */
async function sessionFixture(page: Page, granted = permissions) {
  await page.route("**/portcullis.v1.Auth/Me", route => answer(route, MeResponseSchema, { user: { id: actor, email: "operator@example.com", status: "active" }, permissions: granted }));
}

const pagination = { page: 1, pageSize: 20, totalCount: 1n, authorizationRevision: 7n };
const source = { title: "Preserve me", expression: 'principal.id == "private-marker"', languageVersion: "cel-v1", environmentVersion: "portcullis-v1" };

/** Adds authorized list and detail fixtures independently of write permissions. */
async function readFixtures(page: Page) {
  await sessionFixture(page);
  await page.route("**/portcullis.v1.Users/List", route => answer(route, management.ListUsersResponseSchema, { users: [{ id: actor, email: "operator@example.com", status: "active" }, { id: member, email: "member@example.com", status: "active" }], pagination: { ...pagination, totalCount: 2n } }));
  await page.route("**/portcullis.v1.Users/Get", route => answer(route, management.GetUserResponseSchema, { user: { user: { id: member, email: "member@example.com", status: "active" }, hasPassword: false } }));
  await page.route("**/portcullis.v1.Roles/List", route => answer(route, management.ListRolesResponseSchema, { roles: [{ id: roleID, name: "Reader", version: 3n, bindingCount: 1n }], pagination }));
  await page.route("**/portcullis.v1.Roles/Get", route => answer(route, management.GetRoleResponseSchema, { role: { summary: { id: roleID, name: "Reader", version: 3n, bindingCount: 1n }, permissions: ["connections.get"] } }));
  await page.route("**/portcullis.v1.Bindings/List", route => answer(route, management.ListBindingsResponseSchema, { bindings: [{ id: bindingID, userId: member, roleId: roleID, version: 4n, hasCondition: true, scope: { type: "organization" } }], pagination }));
  await page.route("**/portcullis.v1.Bindings/Get", route => answer(route, management.GetBindingResponseSchema, { binding: { summary: { id: bindingID, userId: member, roleId: roleID, version: 4n, hasCondition: true, scope: { type: "organization" } }, condition: source } }));
}

test("management pages support direct links, history, numbered lists and independent details", async ({ page }) => {
  await readFixtures(page);
  await page.goto("/users");
  await expect(page.getByRole("heading", { name: "Users", exact: true })).toBeVisible();
  await expect(page.getByText("1–2 of 2")).toBeVisible();
  await page.getByRole("link", { name: "Details" }).last().click();
  await expect(page).toHaveURL(new RegExp(`/users/${member}$`));
  await page.reload(); await expect(page.getByRole("heading", { name: "User details" })).toBeVisible();
  await page.getByRole("link", { name: "Back to Users" }).click();
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Roles", exact: true }).click();
  await expect(page.getByText("Reader", { exact: true })).toBeVisible();
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Bindings", exact: true }).click();
  await expect(page.getByText("Conditional", { exact: true })).toBeVisible();
  await expect(page.getByText("private-marker", { exact: false })).toHaveCount(0);
  await page.setViewportSize({ width: 320, height: 800 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
});

test("creating users and roles shows narrow receipts and transient setup delivery", async ({ page }) => {
  await readFixtures(page);
  await page.route("**/portcullis.v1.Users/Create", route => answer(route, management.CreateUserResponseSchema, { user: { id: member, status: "active" }, setupLink: { token: "one-time-ui-token", expiresAt: { seconds: 1893456000n } } }));
  await page.goto("/users/new");
  await page.getByLabel("Email", { exact: true }).fill("member@example.com");
  await page.getByLabel("Display name").fill("Member");
  await page.getByRole("button", { name: "Create user", exact: true }).click();
  await expect(page.getByLabel("Setup token")).toHaveValue("one-time-ui-token");
  await expect(page.getByLabel("Created user ID")).toHaveValue(member);
  await page.getByRole("button", { name: "Dismiss setup token" }).click();
  await expect(page.getByLabel("Setup token")).toHaveCount(0);
  await page.route("**/portcullis.v1.Roles/Create", route => {
    const body = route.request().postDataBuffer(); if (body === null) throw new Error("missing request");
    const request = fromBinary(management.CreateRoleRequestSchema, body);
    expect(request.permissions).toEqual(["connections.get"]);
    return answer(route, management.CreateRoleResponseSchema, { role: { id: roleID, version: 1n } });
  });
  await page.goto("/roles/new"); await page.getByLabel("Role name").fill("Reader");
  await page.getByLabel("Permission keys").fill("connections.get");
  await page.getByRole("button", { name: "Create role", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Created");
  await expect(page.getByLabel("Role ID receipt")).toHaveValue(roleID);
});

test("binding form validates generated CEL before sending a scoped grant", async ({ page }) => {
  await readFixtures(page); let validated = false;
  await page.route("**/portcullis.v1.Bindings/ValidateCondition", route => { validated = true; return answer(route, management.ValidateBindingConditionResponseSchema, {}); });
  await page.route("**/portcullis.v1.Bindings/Create", route => {
    const body = route.request().postDataBuffer(); if (body === null) throw new Error("missing request");
    const request = fromBinary(management.CreateBindingRequestSchema, body);
    expect(validated).toBe(true); expect(request.scope?.type).toBe("connection");
    expect(request.condition?.expression).toContain('resource.type == "request"');
    return answer(route, management.CreateBindingResponseSchema, { binding: { id: bindingID, version: 1n }, authorizationRevision: 8n });
  });
  await page.goto("/bindings/new");
  await page.getByLabel("User ID", { exact: true }).fill(member); await page.getByLabel("Role ID", { exact: true }).fill(roleID);
  await page.getByLabel("Scope", { exact: true }).selectOption("connection");
  await page.getByLabel("Connection ID", { exact: true }).fill("00000000-0000-4000-8000-000000000005");
  await page.getByLabel("Condition change").selectOption("replace");
  await page.getByLabel("Condition title").fill("Requests only"); await page.getByLabel("Resource type").selectOption("request");
  await page.setViewportSize({ width: 320, height: 900 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
  await page.screenshot({ path: test.info().outputPath("administration-binding-light.png"), fullPage: true });
  await page.evaluate(() => document.documentElement.classList.add("dark"));
  await page.screenshot({ path: test.info().outputPath("administration-binding-dark.png"), fullPage: true });
  await page.getByRole("button", { name: "Create binding", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Created");
  await expect(page.getByLabel("Binding ID receipt")).toHaveValue(bindingID);
});

test("binding edits preserve stored source and carry the displayed optimistic version", async ({ page }) => {
  await readFixtures(page); let writes = 0;
  await page.route("**/portcullis.v1.Bindings/Update", route => {
    const body = route.request().postDataBuffer(); if (body === null) throw new Error("missing request");
    const request = fromBinary(management.UpdateBindingRequestSchema, body);
    expect(request.expectedVersion).toBe(4n); expect(request.conditionEdit.case).toBeUndefined(); writes++;
    return answer(route, management.UpdateBindingResponseSchema, { binding: { id: bindingID, version: 5n } });
  });
  await page.goto(`/bindings/${bindingID}`); await expect(page.getByText(source.expression, { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Save binding", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Saved"); expect(writes).toBe(1);
  await expect(page.getByRole("button", { name: "Save binding", exact: true })).toBeDisabled();
});

test("self status controls are hidden and last administrator refusals never imply success", async ({ page }) => {
  await readFixtures(page);
  await page.route("**/portcullis.v1.Users/Disable", route => route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ code: "failed_precondition", message: "secret diagnostic" }) }));
  await page.goto("/users");
  await expect(page.getByRole("button", { name: "Disable user" })).toHaveCount(1);
  await page.getByRole("button", { name: "Disable user" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Confirm" }).click();
  await expect(page.getByRole("alert")).toContainText("last administrator");
  await expect(page.getByText("secret diagnostic")).toHaveCount(0);
});

test("server validation refusal preserves draft and never sends an unconditional binding", async ({ page }) => {
  await readFixtures(page); let writes = 0;
  await page.route("**/portcullis.v1.Bindings/ValidateCondition", route => route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ code: "invalid_argument", message: "private validation diagnostic" }) }));
  await page.route("**/portcullis.v1.Bindings/Create", async route => { writes++; await answer(route, management.CreateBindingResponseSchema, {}); });
  await page.goto("/bindings/new");
  await page.getByLabel("User ID", { exact: true }).fill(member); await page.getByLabel("Role ID", { exact: true }).fill(roleID);
  await page.getByLabel("Condition change").selectOption("replace"); await page.getByLabel("Condition title").fill("Keep draft");
  await page.getByLabel("Resource type").selectOption("request");
  await page.getByRole("button", { name: "Create binding", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Invalid input");
  await expect(page.getByLabel("Condition title")).toHaveValue("Keep draft");
  await expect(page.getByText("private validation diagnostic")).toHaveCount(0);
  expect(writes).toBe(0);
});

test("stale roles require explicit reload and unknown condition contracts stay read only", async ({ page }) => {
  await readFixtures(page);
  await page.route("**/portcullis.v1.Roles/Update", route => route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ code: "aborted", message: "changed" }) }));
  await page.goto(`/roles/${roleID}`); await page.getByLabel("Role name").fill("Retained draft");
  await page.getByRole("button", { name: "Save role", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Reload");
  await expect(page.getByLabel("Role name")).toHaveValue("Retained draft");
  await expect(page.getByRole("button", { name: "Save role", exact: true })).toBeDisabled();
  await page.route("**/portcullis.v1.Bindings/Get", route => answer(route, management.GetBindingResponseSchema, { binding: { summary: { id: bindingID, userId: member, roleId: roleID, version: 4n, scope: { type: "organization" } }, condition: { ...source, languageVersion: "cel-v2" } } }));
  await page.goto(`/bindings/${bindingID}`);
  await expect(page.getByText(source.expression, { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Save binding", exact: true })).toHaveCount(0);
});

test("list-only readers never receive detail actions and denied refreshes clear rows and totals", async ({ page }) => {
  await readFixtures(page); await sessionFixture(page, ["users.list"]);
  await page.goto("/users"); await expect(page.getByText("member@example.com", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Details" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Create user" })).toHaveCount(0);
  await page.route("**/portcullis.v1.Users/List", route => fulfillConnectError(route, "permission_denied"));
  await page.getByRole("button", { name: "Refresh" }).click();
  await expect(page.getByRole("alert")).toContainText("Permission");
  await expect(page.getByText("member@example.com", { exact: true })).toHaveCount(0);
  await expect(page.getByText("1–2 of 2")).toHaveCount(0);
});


test("pagination carries the current revision and a stale continuation clears rows until explicit reload", async ({ page }) => {
  await readFixtures(page); let firstPageLoads = 0;
  await page.route("**/portcullis.v1.Users/List", route => {
    const body = route.request().postDataBuffer(); if (body === null) throw new Error("missing request");
    const request = fromBinary(management.ListUsersRequestSchema, body);
    if (request.pagination?.page === 2) {
      expect(request.pagination.expectedRevision).toBe(7n);
      return route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ code: "aborted", message: "changed" }) });
    }
    firstPageLoads++;
    return answer(route, management.ListUsersResponseSchema, { users: [{ id: member, email: "page-one@example.com", status: "active" }], pagination: { ...pagination, totalCount: 21n } });
  });
  await page.goto("/users"); await expect(page.getByText("1–20 of 21")).toBeVisible(); expect(firstPageLoads).toBe(1);
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Reload");
  await expect(page.getByText("page-one@example.com")).toHaveCount(0); expect(firstPageLoads).toBe(1);
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.getByText("page-one@example.com")).toBeVisible(); expect(firstPageLoads).toBe(2);
});

test("condition removal requires confirmation and never strips source on cancel", async ({ page }) => {
  await readFixtures(page); let writes = 0;
  await page.route("**/portcullis.v1.Bindings/Update", route => {
    const body = route.request().postDataBuffer(); if (body === null) throw new Error("missing request");
    const request = fromBinary(management.UpdateBindingRequestSchema, body);
    expect(request.conditionEdit).toEqual({ case: "clearCondition", value: true }); writes++;
    return answer(route, management.UpdateBindingResponseSchema, { binding: { id: bindingID, version: 5n } });
  });
  await page.goto(`/bindings/${bindingID}`);
  await page.getByLabel("Condition change").selectOption("clear");
  await page.getByRole("button", { name: "Remove condition and save", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Cancel", exact: true }).click();
  expect(writes).toBe(0); await expect(page.getByText(source.expression, { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Remove condition and save", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Saved"); expect(writes).toBe(1);
});

test("setup reissue and successful status, deletion and revocation use independent operations", async ({ page }) => {
  await readFixtures(page); const operations: string[] = [];
  await page.route("**/portcullis.v1.Users/IssueSetupLink", route => { operations.push("setup"); return answer(route, management.IssueUserSetupLinkResponseSchema, { setupLink: { token: "replacement-ui-token" } }); });
  await page.route("**/portcullis.v1.Users/Disable", route => { operations.push("disable"); return answer(route, management.DisableUserResponseSchema, { user: { id: member, status: "disabled" } }); });
  await page.goto(`/users/${member}`); await page.getByRole("button", { name: "Issue setup token" }).click();
  await expect(page.getByLabel("Setup token")).toHaveValue("replacement-ui-token");
  await page.getByRole("button", { name: "Disable user", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Confirm" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved account" })).toBeVisible();
  await page.getByRole("link", { name: "Back to Users" }).click(); await expect(page.getByLabel("Setup token")).toHaveCount(0);
  await page.route("**/portcullis.v1.Roles/Get", route => answer(route, management.GetRoleResponseSchema, { role: { summary: { id: roleID, name: "Unused", version: 3n }, permissions: [] } }));
  await page.route("**/portcullis.v1.Roles/Delete", route => { operations.push("delete"); return answer(route, management.DeleteRoleResponseSchema, {}); });
  await page.goto(`/roles/${roleID}`); await page.getByRole("button", { name: "Delete role", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Confirm" }).click(); await expect(page.getByRole("status")).toContainText("Deleted");
  await page.route("**/portcullis.v1.Bindings/Revoke", route => { operations.push("revoke"); return answer(route, management.RevokeBindingResponseSchema, { binding: { id: bindingID, version: 5n } }); });
  await page.goto("/bindings"); await page.getByRole("button", { name: "Revoke binding", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Confirm" }).click(); await expect(page.getByRole("status")).toContainText("Revoked");
  expect(operations).toEqual(["setup", "disable", "delete", "revoke"]);
});

test("create-only roles do not probe lists and inaccessible or system details expose no edit affordance", async ({ page }) => {
  await readFixtures(page); await sessionFixture(page, ["roles.create"]); let lists = 0;
  await page.route("**/portcullis.v1.Roles/List", route => { lists++; return answer(route, management.ListRolesResponseSchema, {}); });
  await page.goto("/roles"); await page.getByRole("link", { name: "Create role", exact: true }).click();
  await expect(page.getByLabel("Role name")).toBeVisible(); expect(lists).toBe(0);
  await expect(page.getByRole("button", { name: "Load permission catalog" })).toHaveCount(0);
  await sessionFixture(page);
  await page.route("**/portcullis.v1.Roles/Get", route => answer(route, management.GetRoleResponseSchema, { role: { summary: { id: roleID, name: "System admin", version: 3n, isSystem: true }, permissions: ["roles.get"] } }));
  await page.goto(`/roles/${roleID}`); await expect(page.getByText("System role — read only")).toBeVisible();
  await expect(page.getByRole("button", { name: "Save role", exact: true })).toHaveCount(0); await expect(page.getByRole("button", { name: "Delete role", exact: true })).toHaveCount(0);
  await page.route("**/portcullis.v1.Roles/Get", route => fulfillConnectError(route, "not_found", "private condition or profile"));
  await page.reload(); await expect(page.getByRole("alert")).toContainText("Resource unavailable");
  await expect(page.getByText("System admin")).toHaveCount(0); await expect(page.getByText("private condition or profile")).toHaveCount(0);
});
