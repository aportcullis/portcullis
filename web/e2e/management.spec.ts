import { expect, test } from "@playwright/test";

import { readSetupToken, sameOriginHeaders, signInForScenario } from "@e2e/login";

test("prepared management APIs enforce sessions and CSRF while preserving active authority", async ({ page, baseURL }) => {
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
  const authorityBefore: unknown = await before.json();

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
      const response = await page.request.post(`/portcullis.v1.${service.name}/${method}`, { headers: authenticatedHeaders, data: {} });
      expect(response.status(), `${service.name}.${method} stays inactive`).toBe(503);
      expect(response.headers()["cache-control"]).toBe("no-store");
      expect(await response.json()).toHaveProperty("code", "unavailable");
    }
  }
  const after = await page.request.post("/portcullis.v1.Auth/Me", { headers: authenticatedHeaders, data: {} });
  expect(after.status()).toBe(200);
  expect(await after.json()).toEqual(authorityBefore);
});
