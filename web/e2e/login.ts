import { expect, type Page } from "@playwright/test";

// ADR-0010's credential buckets refill one token per three seconds.
const credentialRefillDelayMs = 3_200;
const maxScenarioLoginAttempts = 3;

/** Signs in through the real form, waiting only for bounded credential-rate-limit recovery. */
export async function signInForScenario(page: Page, email: string, password: string, landing: RegExp = /\/connections$/): Promise<void> {
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  for (let attempt = 0; attempt < maxScenarioLoginAttempts; attempt++) {
    const response = page.waitForResponse(response =>
      response.request().method() === "POST" && response.url().endsWith("/portcullis.v1.Auth/Login"));
    await page.getByRole("button", { name: "Sign in" }).click();
    const status = (await response).status();
    if (status === 429 && attempt + 1 < maxScenarioLoginAttempts) {
      await page.waitForTimeout(credentialRefillDelayMs);
      continue;
    }
    expect(status, "scenario login must reach authentication successfully").toBe(200);
    await expect(page).toHaveURL(landing);
    return;
  }
}
