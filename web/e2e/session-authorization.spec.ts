import { create, fromBinary, toBinary } from "@bufbuild/protobuf";
import { expect, test, type Page } from "@playwright/test";

import { LoginResponseSchema, MeResponseSchema } from "@/gen/portcullis/v1/auth_pb";
import { readSetupToken, signInForScenario } from "@e2e/login";

/** Starts at real authentication, including the empty-installation path in isolated runs. */
async function openSignIn(page: Page): Promise<void> {
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
}

test("a committed login with unavailable access metadata recovers through Me without replaying credentials", async ({ page }) => {
  await openSignIn(page);
  let credentialWrites = 0;
  page.on("request", request => { if (request.url().endsWith("/portcullis.v1.Auth/Login")) credentialWrites++; });
  await page.route("**/portcullis.v1.Auth/Login", async route => {
    const response = await route.fetch();
    if (response.status() !== 200) { await route.fulfill({ response }); return; }
    const committed = fromBinary(LoginResponseSchema, await response.body());
    const unavailable = create(LoginResponseSchema, { user: committed.user, authorizationUnavailable: true });
    await route.fulfill({ response, body: Buffer.from(toBinary(LoginResponseSchema, unavailable)) });
  });
  await signInForScenario(page, "admin@example.com", "correct-horse-battery", /\/$/);
  await expect(page.getByRole("heading", { name: "Access information is temporarily unavailable" })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Main" })).toHaveCount(0);
  await expect(page.getByText(/check your email and password/)).toHaveCount(0);
  const completedWrites = credentialWrites;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Connections", exact: true })).toBeVisible();
  expect(credentialWrites).toBe(completedWrites);
});

test("failed metadata refresh removes protected UI and permits recovery with the same session", async ({ page }) => {
  await openSignIn(page);
  await signInForScenario(page, "admin@example.com", "correct-horse-battery");
  await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
  const pattern = "**/portcullis.v1.Auth/Me";
  await page.route(pattern, async route => {
    const response = await route.fetch();
    if (response.status() !== 200) { await route.fulfill({ response }); return; }
    const authenticated = fromBinary(MeResponseSchema, await response.body());
    await route.fulfill({ response, body: Buffer.from(toBinary(MeResponseSchema, create(MeResponseSchema, { user: authenticated.user, authorizationUnavailable: true }))) });
  });
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(page.getByRole("heading", { name: "Access information is temporarily unavailable" })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Main" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Create connection", exact: true })).toHaveCount(0);
  await expect(page).toHaveURL(/\/connections$/);
  await page.unroute(pattern);
  let credentialWrites = 0;
  page.on("request", request => { if (request.url().endsWith("/portcullis.v1.Auth/Login")) credentialWrites++; });
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Connections", exact: true })).toBeVisible();
  expect(credentialWrites).toBe(0);
});
