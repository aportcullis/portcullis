import { expect, test, type Page } from "@playwright/test";
import { readSetupToken, signInForScenario, sameOriginHeaders } from "@e2e/login";

/** Makes explicit form retries only for confirmed pre-admission throttling. */
async function submitPasswordSetup(page: Page, expectedStatus: number): Promise<void> {
 for (let attempt = 0; attempt < 3; attempt++) {
  const completed = page.waitForResponse(response => response.url().endsWith("/portcullis.v1.Auth/CompletePasswordSetup"));
  await page.getByRole("button", { name: "Set password", exact: true }).click();
  const response = await completed;
  if (response.status() === 429 && attempt < 2) { await expect(page.getByRole("alert")).toContainText("busy"); await page.waitForTimeout(3200); continue; }
  expect(response.status()).toBe(expectedStatus); expect(response.headers()["set-cookie"]).toBeUndefined(); return;
 }
}

test("issued password link completes once without leaking its fragment or starting a session", async ({ page, browser, baseURL }) => {
 await page.goto("/");
 await expect(page).toHaveURL(/\/(?:bootstrap|login)$/);
 if (page.url().endsWith("/bootstrap")) {
  await page.getByLabel("Setup token").fill(await readSetupToken()); await page.getByLabel("Email").fill("admin@example.com"); await page.getByLabel("Display name").fill("Admin"); await page.getByLabel(/^Password/).fill("correct-horse-battery"); await page.getByRole("button", { name: "Create admin account" }).click(); await expect(page).toHaveURL(/\/login$/);
 }
 await signInForScenario(page, "admin@example.com", "correct-horse-battery");
 await page.goto("/users/new"); const email = `setup-${Date.now()}@example.com`;
 await page.getByLabel("Email", { exact: true }).fill(email); await page.getByLabel("Display name", { exact: true }).fill("Setup member"); await page.getByRole("button", { name: "Create user", exact: true }).click();
 const link = await page.getByLabel("Setup link", { exact: true }).inputValue(); const token = new URL(link).hash.slice(7);
 expect(new URL(link).origin).toBe(baseURL); expect(new URL(link).search).toBe("");
 const memberContext = await browser.newContext({ baseURL });
 try {
  const member = await memberContext.newPage(); const urls: string[] = []; member.on("request", request => { urls.push(request.url()); });
  const document = await member.goto(link); expect(document?.headers()["referrer-policy"]).toBe("no-referrer");
  await expect(member.getByRole("heading", { name: "Set your password" })).toBeVisible(); await expect(member).toHaveURL(/\/setup-password$/);
  await member.reload(); await expect(member.getByRole("alert")).toContainText("Invalid or expired setup link"); await member.goto(link);
  const deniedOrigins: Record<string, string>[] = [{ Origin: "https://foreign.example" }, {}];
  for (const headers of deniedOrigins) {
    const refused = await member.request.post("/portcullis.v1.Auth/CompletePasswordSetup", { headers, data: { token, password: "another long password" } }); expect(refused.status()).toBe(403);
  }
  await member.getByLabel("New password", { exact: true }).fill("correct-horse-battery"); await member.getByLabel("Confirm password", { exact: true }).fill("a different password"); await member.getByRole("button", { name: "Set password", exact: true }).click(); await expect(member.getByRole("alert")).toContainText("match");
  await member.getByLabel("Confirm password", { exact: true }).fill("correct-horse-battery");
  await submitPasswordSetup(member, 200);
  await expect(member.getByRole("status")).toContainText("Password set"); expect(urls.some(url => url.includes(token))).toBe(false);
  expect(await member.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(token);
  expect((await member.request.post("/portcullis.v1.Auth/Me", { headers: sameOriginHeaders(baseURL), data: {} })).status()).toBe(401);
  await member.getByRole("link", { name: "Sign in", exact: true }).click(); await signInForScenario(member, email, "correct-horse-battery", /\/requests$/);
  await memberContext.clearCookies(); await member.goto(link); await member.getByLabel("New password", { exact: true }).fill("another long password"); await member.getByLabel("Confirm password", { exact: true }).fill("another long password"); await submitPasswordSetup(member, 403);
  await expect(member.getByRole("alert")).toContainText("Invalid or expired setup link"); await expect(member.getByLabel("New password", { exact: true })).toHaveCount(0);
 } finally { await memberContext.close(); }
});
