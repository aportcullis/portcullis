import { expect, test } from "@playwright/test";

import { signInForScenario } from "@e2e/login";


test.describe.serial("auth vertical", () => {
  const email = "admin@example.com";
  const displayName = "Admin";
  const password = "correct-horse-battery";
  const wrong = "wrong-password-xx";


  const genericError = "We couldn't sign you in. Please check your email and password and try again.";

  const signIn = async (page: import("@playwright/test").Page, pw: string) => {
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(pw);
    await page.getByRole("button", { name: "Sign in" }).click();
  };

  test("first run: bootstrap → login → session survives reload → logout", async ({ page }) => {

    await page.goto("/");
    await expect(page).toHaveURL(/\/bootstrap$/);

    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Display name").fill(displayName);
    await page.getByLabel(/^Password/).fill(password);


    await expect(page.getByLabel(/^Password/)).toHaveAttribute("type", "password");
    await page.getByRole("button", { name: "Show password" }).click();
    await expect(page.getByLabel(/^Password/)).toHaveAttribute("type", "text");
    await expect(page.getByLabel(/^Password/)).toHaveValue(password);
    await page.getByRole("button", { name: "Hide password" }).click();
    await expect(page.getByLabel(/^Password/)).toHaveAttribute("type", "password");

    await page.getByRole("button", { name: "Create admin account" }).click();


    await expect(page).toHaveURL(/\/login$/);
    await signInForScenario(page, email, password);
    await expect(page).toHaveURL(/\/connections$/);
    await expect(page.getByText(displayName, { exact: true })).toBeVisible();

    await expect(page.getByText("admin", { exact: true })).toBeVisible();


    await page.reload();
    await expect(page.getByText(displayName, { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/login$/);
    await page.reload();
    await expect(page).toHaveURL(/\/login$/);
  });

  test("a stale CSRF cookie routes to login, not a stuck state", async ({ page }) => {
    // Missing CSRF invalidates the session view and routes to login, rather than outage retry.
    await page.goto("/login");
    await signInForScenario(page, email, password);
    await expect(page.getByText(displayName, { exact: true })).toBeVisible();

    await page.context().clearCookies({ name: "__Host-portcullis_csrf" });
    await page.reload();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByText("temporarily unreachable")).toHaveCount(0);
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

    // This server has no Google login configured: the button must be hidden (GetConfig-driven), and there is never a Google JS SDK (ADR-0007 — the real button is a plain backend anchor).
    await expect(page.getByText("Continue with Google")).toHaveCount(0);
    for (const script of await page.locator("script[src]").all()) {
      const src = await script.getAttribute("src");
      expect(src, "external SDK script").not.toMatch(/^https?:/);
    }


    await expect(page.getByText(/First run/)).toHaveCount(0);
    await page.goto("/bootstrap");
    await expect(page).toHaveURL(/\/login$/);
  });

  // The progressive-backoff lockout scenario lives in zz-auth-lockout.spec.ts: it locks the admin account for over a minute, so it must run LAST — files run alphabetically, and connections.spec.ts needs a working admin login.
});
