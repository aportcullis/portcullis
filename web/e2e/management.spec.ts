import { fromJsonString } from "@bufbuild/protobuf";
import { MeResponseSchema } from "@/gen/portcullis/v1/auth_pb";

import { expect, test } from "@playwright/test";

import { readSetupToken, sameOriginHeaders, signInForScenario } from "@e2e/login";

test("active management preserves grants and protects real user, Role and Binding workflows", async ({ page, baseURL }) => {
  const headers = { ...sameOriginHeaders(baseURL), "Content-Type": "application/json", "Connect-Protocol-Version": "1" };
  const anonymous = await page.request.post("/portcullis.v1.Users/List", { headers, data: {} });
  expect(anonymous.status()).toBe(401);
  expect(anonymous.headers()["cache-control"]).toBe("no-store");

  await page.goto("/");
  await expect(page).toHaveURL(/\/(?:bootstrap|login)$/);
  if (page.url().endsWith("/bootstrap")) {
    await page.getByLabel("Setup token").fill(await readSetupToken());
    await page.getByLabel("Email").fill("admin@example.com");
    await page.getByLabel("Display name").fill("Admin");
    await page.getByLabel(/^Password/).fill("correct-horse-battery");
    await page.getByRole("button", { name: "Create admin account" }).click();
    await expect(page).toHaveURL(/\/login$/);
  }
  await signInForScenario(page, "admin@example.com", "correct-horse-battery");
  const cookies = await page.context().cookies();
  const token = cookies.find(cookie => cookie.name === "__Host-portcullis_csrf");
  if (token === undefined) throw new Error("login must issue a CSRF cookie");
  if (!cookies.some(cookie => cookie.name === "__Host-portcullis_session")) throw new Error("login must issue a session cookie");
  // Forward Chromium's Secure localhost cookies to the raw Node request client.
  const sessionHeaders = { ...headers, Cookie: cookies.map(cookie => `${cookie.name}=${cookie.value}`).join("; ") };
  const authenticatedHeaders = { ...sessionHeaders, "X-CSRF-Token": token.value };
  const before = await page.request.post("/portcullis.v1.Auth/Me", { headers: authenticatedHeaders, data: {} });
  expect(before.status()).toBe(200);
  const authorityBefore = fromJsonString(MeResponseSchema, await before.text());

  const missingCSRF = await page.request.post("/portcullis.v1.Roles/List", { headers: sessionHeaders, data: {} });
  expect(missingCSRF.status()).toBe(403);
  const forgedCSRF = await page.request.post("/portcullis.v1.Bindings/List", { headers: { ...sessionHeaders, "X-CSRF-Token": "forged" }, data: {} });
  expect(forgedCSRF.status()).toBe(403);

  const services: { name: string; methods: string[] }[] = [
    { name: "Users", methods: ["List", "Get", "Create", "Disable", "Enable", "IssueSetupLink"] },
    { name: "Roles", methods: ["List", "Get", "ListPermissions", "Create", "Update", "Delete"] },
    { name: "Bindings", methods: ["List", "Get", "Create", "Update", "Revoke", "ValidateCondition"] },
  ];
  for (const service of services) {
    for (const method of service.methods) {
      const response = await page.request.post(`/portcullis.v1.${service.name}/${method}`, { headers: sessionHeaders, data: {} });
      expect(response.status(), `${service.name}.${method} requires CSRF`).toBe(403);
      expect(response.headers()["cache-control"]).toBe("no-store");
    }
  }
  for (const service of ["Users", "Roles", "Bindings"]) {
    const response = await page.request.post(`/portcullis.v1.${service}/List`, { headers: authenticatedHeaders, data: {} });
    expect(response.status()).toBe(200);
    expect(response.headers()["cache-control"]).toBe("no-store");
  }
  const suffix = Date.now().toString();
  await page.goto("/users/new");
  await page.getByLabel("Email", { exact: true }).fill(`activation-${suffix}@example.com`);
  await page.getByLabel("Display name", { exact: true }).fill("Activation member");
  await page.getByRole("button", { name: "Create user", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Created user without grants");
  const memberID = await page.getByLabel("Created user ID").inputValue();
  await expect(page.getByLabel("Setup token", { exact: true })).not.toHaveValue("");

  await page.goto("/roles/new");
  await expect(page.getByLabel("Setup token", { exact: true })).toHaveCount(0);
  await page.getByLabel("Role name").fill(`Activation reader ${suffix}`);
  await page.getByLabel("Permission keys").fill("requests.get");
  await page.getByRole("button", { name: "Create role", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Created role");
  const roleID = await page.getByLabel("Role ID receipt").inputValue();

  await page.goto("/bindings/new");
  await page.getByLabel("User ID", { exact: true }).fill(memberID);
  await page.getByLabel("Role ID", { exact: true }).fill(roleID);
  await page.getByLabel("Condition change").selectOption("replace");
  await page.getByLabel("Condition title").fill("Future request window");
  await page.getByLabel("Expires at (ISO timestamp)").fill("2999-01-01T00:00:00Z");
  await page.getByLabel("Resource type", { exact: true }).selectOption("request");
  await page.getByRole("button", { name: "Create binding", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Created binding");
  const bindingID = await page.getByLabel("Binding ID receipt").inputValue();
  await page.goto(`/bindings/${bindingID}`);
  await expect(page.getByText("Future request window", { exact: false }).first()).toBeVisible();
  await page.getByRole("button", { name: "Revoke binding", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Revoked binding");

  await page.goto(`/roles/${roleID}`);
  await page.getByRole("button", { name: "Delete role", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Deleted role");
  await page.goto(`/users/${memberID}`);
  await page.getByRole("button", { name: "Disable user", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Saved account state");

  const ownBinding = authorityBefore.bindings.find(binding => binding.scopeType === "organization" && !binding.hasCondition);
  if (ownBinding === undefined) throw new Error("bootstrap must establish an unconditional recovery Binding");
  const refused = await page.request.post("/portcullis.v1.Bindings/Revoke", { headers: authenticatedHeaders, data: { id: ownBinding.id, expectedVersion: "1" } });
  expect(refused.status()).toBe(403);
  const after = await page.request.post("/portcullis.v1.Auth/Me", { headers: authenticatedHeaders, data: {} });
  expect(after.status()).toBe(200);
  const authorityAfter = fromJsonString(MeResponseSchema, await after.text());
  expect(authorityAfter.permissions).toEqual(authorityBefore.permissions);
  expect(authorityAfter.capabilities).toEqual(authorityBefore.capabilities);
  expect(authorityAfter.bindings).toEqual(authorityBefore.bindings);
  expect(authorityAfter.authorizationRevision).toBeGreaterThan(authorityBefore.authorizationRevision);
});
