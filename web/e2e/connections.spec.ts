import { expect, test } from "@playwright/test";

import type { Target } from "./target";
import { loadTarget } from "./target";

// The connections vertical in a real browser (ADR-0014): register a connection
// against the e2e stack's own PostgreSQL, with the pre-save test, the
// relaxed-TLS warning, the uniform redacted failure, and archive. Runs AFTER
// auth.spec.ts (alphabetical), which bootstrapped admin@example.com, and
// BEFORE zz-auth-lockout.spec.ts, which locks that account.

test.describe.serial("connections vertical", () => {
  const email = "admin@example.com";
  const password = "correct-horse-battery";

  const fillConfig = async (
    page: import("@playwright/test").Page,
    target: Target,
    dbPassword: string,
  ) => {
    await page.getByLabel("Host").fill(target.host);
    await page.getByLabel("Port").fill(String(target.port));
    await page.getByLabel("Database").fill(target.database);
    await page.getByLabel("User").fill(target.user);
    await page.getByLabel("Password", { exact: true }).fill(dbPassword);
    // The container has no TLS: choose the relaxed mode explicitly (ADR-0014).
    await page.getByLabel("TLS mode").selectOption("disable");
  };

  test("register → test → archive against a live target", async ({ page }) => {
    const target = loadTarget();
    // Sign in as the admin bootstrapped by auth.spec.ts.
    await page.goto("/login");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    // Login lands straight on the connections table (AppShell layout).
    await expect(page).toHaveURL(/\/connections$/);
    await expect(page.getByText(email, { exact: true })).toBeVisible();
    await expect(page.getByText("No connections yet")).toBeVisible();

    // Open the form; mark it production with a description, and choosing a
    // relaxed TLS mode surfaces the explicit warning.
    await page.getByRole("button", { name: "New connection" }).click();
    await page.getByLabel("Display name").fill("Primary");
    await page.getByLabel("Environment").selectOption("production");
    await page.getByLabel("Description").fill("primary OLTP — e2e");
    await fillConfig(page, target, target.password);
    await expect(page.getByText(/skips certificate validation/)).toBeVisible();

    // Pre-save test succeeds against the live target, then create.
    await page.getByRole("button", { name: "Test connection" }).click();
    await expect(page.getByText("Connection test succeeded.")).toBeVisible();
    await page.getByRole("button", { name: "Create" }).click();

    // The list-safe row appears with its summary and badges; target coordinates
    // and TLS details require the separate detail permission.
    const row = page.getByRole("row", { name: /Primary/ });
    await expect(row).toBeVisible();
    await expect(row.getByText("postgresql")).toBeVisible();
    await expect(row.getByText("active")).toBeVisible();
    // The production label is unmissable in the list (D8-0c).
    await expect(row.getByText("production")).toBeVisible();

    // Details crosses the deliberate list/get permission boundary and restores
    // visibility of the safe descriptor without ever exposing the credential.
    await row.getByRole("button", { name: "Details" }).click();
    await expect(page.getByText(`Host`)).toBeVisible();
    await expect(page.getByText(target.host, { exact: true })).toBeVisible();
    await expect(page.getByText(`TLS mode`)).toBeVisible();
    await expect(page.getByText(`disable`, { exact: true })).toBeVisible();
    await expect(page.getByText("primary OLTP — e2e")).toBeVisible();
    await page.getByRole("button", { name: "Dismiss" }).click();

    // Row-level test dials with the STORED credential.
    await row.getByRole("button", { name: "Test" }).click();
    await expect(row.getByText("OK")).toBeVisible();

    // Edit, flow 1: descriptor-only (no config fields, no re-test — ADR-0014).
    // The dialog prefills from the LIST SUMMARY — no connections.get round-trip
    // (self-review F3), so the stored description must already be in the form.
    await row.getByRole("button", { name: "Edit" }).click();
    await expect(page.getByLabel("Description")).toHaveValue("primary OLTP — e2e");
    await page.getByLabel("Display name").fill("Primary (renamed)");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("row", { name: /Primary \(renamed\)/ })).toBeVisible();

    // Edit, flow 2: full config replacement — the credential is re-entered and
    // the server re-tests before saving; success proves the round trip.
    await row.getByRole("button", { name: "Edit" }).click();
    await page.getByLabel(/Replace connection config/).check();
    await fillConfig(page, target, target.password);
    await page.getByRole("button", { name: "Save" }).click();
    await expect(row.getByText("active")).toBeVisible();

    // A wrong DB password fails the pre-save test with the redacted bucket
    // only, and Create refuses with the same classification.
    await page.getByRole("button", { name: "New connection" }).click();
    await page.getByLabel("Display name").fill("Broken");
    await fillConfig(page, target, "definitely-wrong-password");
    await page.getByRole("button", { name: "Test connection" }).click();
    await expect(page.getByText("Connection test failed: auth-failed.")).toBeVisible();
    await expect(page.getByText(/definitely-wrong-password/)).toHaveCount(0);
    await page.getByRole("button", { name: "Create" }).click();
    await expect(page.getByText("connection test failed: auth-failed")).toBeVisible();
    // Kobalte's dialog close button announces itself as "Dismiss".
    await page.getByRole("button", { name: "Dismiss" }).click();

    // Archive: the confirm dialog spells out the credential discard, and the
    // row flips to archived. Test and re-archive disappear; rename-only Edit
    // survives — an archived row's name labels its history (external review).
    await row.getByRole("button", { name: "Archive" }).click();
    await expect(page.getByText(/permanently discards the stored/)).toBeVisible();
    await page.getByRole("button", { name: "Archive connection" }).click();
    await expect(row.getByText("archived")).toBeVisible();
    await expect(row.getByRole("button", { name: "Test" })).toHaveCount(0);
    await expect(row.getByRole("button", { name: "Archive" })).toHaveCount(0);
    await expect(row.getByRole("button", { name: "Edit" })).toBeVisible();

    // Session change clears the cached list (module store watches the session
    // — a later login must never see another principal's cached descriptors);
    // signing back in re-fetches from the server. Cross-USER disclosure cannot
    // be driven in browser e2e (no user-management API yet) — the Go transport
    // e2e covers the server-side denial; this covers the client-side reset.
    await page.goto("/");
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/login$/);
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await page.getByRole("link", { name: /Connections/ }).click();
    await expect(
      page.getByRole("row", { name: /Primary \(renamed\)/ }).getByText("archived"),
    ).toBeVisible();
  });
});
