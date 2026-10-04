import { expect, test } from "@playwright/test";


test.describe.serial("auth lockout", () => {
  const email = "admin@example.com";
  const password = "correct-horse-battery";
  const wrong = "wrong-password-xx";


  const genericError = "We couldn't sign you in. Please check your email and password and try again.";


  const slowExpect = { timeout: 15_000 };

  const signIn = async (page: import("@playwright/test").Page, pw: string) => {
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(pw);
    const response = page.waitForResponse(res =>
      res.request().method() === "POST" && res.url().endsWith("/portcullis.v1.Auth/Login"));
    await page.getByRole("button", { name: "Sign in" }).click();
    expect((await response).status(), "credential rejection must reach authentication, not the rate limiter").toBe(401);
  };

  test("progressive backoff lockout is invisible", async ({ page }) => {
    test.slow();
    await page.goto("/login");

    // The preceding scenarios can drain the shared IP bucket. Refill before the first attempt as well as between attempts; a 429 never increments the account's failure counter (ADR-0006/0010).
    await page.waitForTimeout(3200);
    for (let attempt = 0; attempt < 5; attempt++) {
      await signIn(page, wrong);
      await expect(page.getByText(genericError)).toBeVisible(slowExpect);
      await page.waitForTimeout(3200);
    }

    // The CORRECT password now fails with the SAME message — the lockout must be indistinguishable from a wrong password (no oracle).
    await signIn(page, password);
    await expect(page.getByText(genericError)).toBeVisible(slowExpect);
    await expect(page).toHaveURL(/\/login$/, slowExpect);
  });
});
