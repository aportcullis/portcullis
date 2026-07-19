import { expect, test } from "@playwright/test";

// Progressive-backoff lockout (ADR-0006), split out of auth.spec.ts: this test
// locks the admin account for over a minute, so the zz- prefix pins it to run
// LAST (Playwright runs files alphabetically with one worker) — every other
// spec needs a working admin login.
test.describe.serial("auth lockout", () => {
  const email = "admin@example.com";
  const password = "correct-horse-battery"; // the admin bootstrapped in auth.spec.ts
  const wrong = "wrong-password-xx";

  // One uniform message for every login rejection — the assertion target.
  const genericError = "We couldn't sign you in. Please check your email and password and try again.";

  // test.slow() only triples the TEST timeout; expect() keeps its independent 5s
  // default ("Assertion timeout is unrelated to the test timeout" — Playwright
  // docs). A login roundtrip includes an Argon2 hash (queued behind a small
  // concurrency cap) and can exceed 5s on slow machines, so every response
  // assertion gets an explicit budget.
  const slowExpect = { timeout: 15_000 };

  const signIn = async (page: import("@playwright/test").Page, pw: string) => {
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(pw);
    await page.getByRole("button", { name: "Sign in" }).click();
  };

  test("progressive backoff lockout is invisible", async ({ page }) => {
    test.slow(); // deliberate pacing: stay under the login rate limit (ADR-0010)
    await page.goto("/login");

    // 5 wrong passwords lock the account (ADR-0006). Pace the attempts so the
    // per-IP/email token bucket refills — this test targets the backoff, and a
    // rate-limited attempt would never reach the failure counter.
    for (let i = 0; i < 5; i++) {
      await signIn(page, wrong);
      await expect(page.getByText(genericError)).toBeVisible(slowExpect);
      await page.waitForTimeout(3200);
    }

    // The CORRECT password now fails with the SAME message — the lockout must
    // be indistinguishable from a wrong password (no oracle).
    await signIn(page, password);
    await expect(page.getByText(genericError)).toBeVisible(slowExpect);
    await expect(page).toHaveURL(/\/login$/, slowExpect);
  });
});
