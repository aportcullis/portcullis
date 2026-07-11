import { expect, test } from "@playwright/test";

// The auth vertical end-to-end in a real browser against the real binary and a
// fresh database (ADR-0013): first-run bootstrap → login → session → logout,
// uniform rejection messages, and an invisible progressive-backoff lockout
// (ADR-0006). Serial: the scenarios build on the bootstrap done in the first.
test.describe.serial("auth vertical", () => {
  const email = "admin@example.com";
  const password = "correct-horse-battery"; // 15+ code points (ADR-0006)
  const wrong = "wrong-password-xx";

  // One uniform message for every login rejection — the assertion target.
  const genericError = "Invalid email or password.";

  const signIn = async (page: import("@playwright/test").Page, pw: string) => {
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(pw);
    await page.getByRole("button", { name: "Sign in" }).click();
  };

  test("first run: bootstrap → login → session survives reload → logout", async ({ page }) => {
    // A fresh instance routes straight to the first-run form (GetConfig).
    await page.goto("/");
    await expect(page).toHaveURL(/\/bootstrap$/);

    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Display name").fill("Admin");
    await page.getByLabel(/^Password/).fill(password);
    await page.getByRole("button", { name: "Create admin account" }).click();

    // Bootstrap does not start a session — the login form follows.
    await expect(page).toHaveURL(/\/login$/);
    await signIn(page, password);
    await expect(page.getByText(`Signed in as ${email}`)).toBeVisible();

    // The session rides the __Host- cookies: a full reload stays signed in.
    await page.reload();
    await expect(page.getByText(`Signed in as ${email}`)).toBeVisible();

    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/login$/);
    await page.reload();
    await expect(page).toHaveURL(/\/login$/);
  });

  test("wrong password and unknown email get the same message; no Google SDK", async ({
    page,
  }) => {
    await page.goto("/login");
    await signIn(page, wrong);
    await expect(page.getByText(genericError)).toBeVisible();

    await page.getByLabel("Email").fill("ghost@example.com");
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByText(genericError)).toBeVisible();

    // This server has no Google login configured: the button must be hidden
    // (GetConfig-driven), and there is never a Google JS SDK (ADR-0007 — the
    // real button is a plain backend anchor).
    await expect(page.getByText("Continue with Google")).toHaveCount(0);
    for (const script of await page.locator("script[src]").all()) {
      const src = await script.getAttribute("src");
      expect(src, "external SDK script").not.toMatch(/^https?:/);
    }
  });

  test("progressive backoff lockout is invisible", async ({ page }) => {
    test.slow(); // deliberate pacing: stay under the login rate limit (ADR-0010)
    await page.goto("/login");

    // 5 wrong passwords lock the account (ADR-0006). Pace the attempts so the
    // per-IP/email token bucket refills — this test targets the backoff, and a
    // rate-limited attempt would never reach the failure counter.
    for (let i = 0; i < 5; i++) {
      await signIn(page, wrong);
      await expect(page.getByText(genericError)).toBeVisible();
      await page.waitForTimeout(3200);
    }

    // The CORRECT password now fails with the SAME message — the lockout must
    // be indistinguishable from a wrong password (no oracle).
    await signIn(page, password);
    await expect(page.getByText(genericError)).toBeVisible();
    await expect(page).toHaveURL(/\/login$/);
  });
});
