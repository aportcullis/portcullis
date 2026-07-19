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
  const genericError = "We couldn't sign you in. Please check your email and password and try again.";

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

    // Bootstrap does not start a session — the login form follows. A login
    // lands straight on the connections table (no interstitial home card);
    // the header carries the signed-in email in its user area.
    await expect(page).toHaveURL(/\/login$/);
    await signIn(page, password);
    await expect(page).toHaveURL(/\/connections$/);
    await expect(page.getByText(email, { exact: true })).toBeVisible();

    // The session rides the __Host- cookies: a full reload stays signed in.
    await page.reload();
    await expect(page.getByText(email, { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/login$/);
    await page.reload();
    await expect(page).toHaveURL(/\/login$/);
  });

  test("a stale CSRF cookie routes to login, not a stuck state", async ({ page }) => {
    // Sign in, then drop only the CSRF cookie — the shape left by a key rotation
    // (ADR-0003) or a partial cookie loss. Me then fails the CSRF check with
    // PermissionDenied; the SPA must treat that as "re-authenticate" and route
    // to /login, NOT trap the user on an unreachable/retry screen (both Me and
    // Logout are CSRF-gated only, never permission-gated).
    await page.goto("/login");
    await signIn(page, password);
    await expect(page.getByText(email, { exact: true })).toBeVisible();

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

    // This server has no Google login configured: the button must be hidden
    // (GetConfig-driven), and there is never a Google JS SDK (ADR-0007 — the
    // real button is a plain backend anchor).
    await expect(page.getByText("Continue with Google")).toHaveCount(0);
    for (const script of await page.locator("script[src]").all()) {
      const src = await script.getAttribute("src");
      expect(src, "external SDK script").not.toMatch(/^https?:/);
    }

    // Once bootstrapped, nothing hints that a first-run flow exists: the login
    // page carries no bootstrap link, and /bootstrap itself bounces to login.
    await expect(page.getByText(/First run/)).toHaveCount(0);
    await page.goto("/bootstrap");
    await expect(page).toHaveURL(/\/login$/);
  });

  // The progressive-backoff lockout scenario lives in zz-auth-lockout.spec.ts:
  // it locks the admin account for over a minute, so it must run LAST — files
  // run alphabetically, and connections.spec.ts needs a working admin login.
});
