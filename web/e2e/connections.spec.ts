import { expect, test } from "@playwright/test";

import { signInForScenario } from "@e2e/login";

import type { Target } from "@e2e/target";
import { loadTarget } from "@e2e/target";



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

    await page.getByLabel("TLS mode").selectOption("disable");
  };

  test("register → test → archive against a live target", async ({ page }) => {
    const target = await loadTarget();

    await page.goto("/login");
    await signInForScenario(page, email, password);
    await expect(page.getByText("Admin", { exact: true })).toBeVisible();
    await expect(page.getByText("No connections yet")).toBeVisible();


    await page.getByRole("button", { name: "New connection" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByLabel("Display name")).toBeFocused();
    await page.getByLabel("Display name").fill("Primary");
    await page.getByLabel("Environment").selectOption("production");
    await page.getByLabel("Description").fill("primary OLTP — e2e");
    await fillConfig(page, target, target.password);
    await expect(page.getByText(/skips certificate validation/)).toBeVisible();


    await page.getByRole("button", { name: "Test connection" }).click();
    await expect(page.getByText("Connection test succeeded.")).toBeVisible();
    await page.getByRole("button", { name: "Create" }).click();


    const row = page.getByRole("row", { name: /Primary/ });
    await expect(row).toBeVisible();
    await expect(row.getByText("postgresql")).toBeVisible();
    await expect(page.getByRole("region", { name: "Edit connection", exact: true })).toHaveCount(0);
    await expect(row.getByText("active")).toBeVisible();

    await expect(row.getByText("production")).toBeVisible();


    await row.getByRole("button", { name: "Details" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByText("Host", { exact: true })).toBeVisible();
    await expect(page.getByText(target.host, { exact: true })).toBeVisible();
    await expect(page.getByText(`TLS mode`)).toBeVisible();
    await expect(page.getByText(`disable`, { exact: true })).toBeVisible();
    await expect(page.getByText("primary OLTP — e2e")).toBeVisible();
    await page.getByRole("button", { name: "Dismiss" }).click();


    await row.getByRole("button", { name: "Test" }).click();
    await expect(row.getByText("OK")).toBeVisible();

    // Edit, flow 1: descriptor-only (no config fields, no re-test — ADR-0014). The dialog prefills from the LIST SUMMARY — no connections.get round-trip, so the stored description must already be in the form.
    await row.getByRole("button", { name: "Edit" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByLabel("Description")).toHaveValue("primary OLTP — e2e");
    await page.getByLabel("Display name").fill("Primary (renamed)");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("region", { name: "Edit connection", exact: true })).toHaveCount(0);
    await expect(page.getByRole("row", { name: /Primary \(renamed\)/ })).toBeVisible();


    await row.getByRole("button", { name: "Edit" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(row.getByRole("button", { name: "Edit" })).toBeDisabled();
    await page.getByLabel(/Replace connection config/).check();
    await fillConfig(page, target, target.password);
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("region", { name: "Edit connection", exact: true })).toHaveCount(0);
    await expect(row.getByText("active")).toBeVisible();

    // A refused save may refresh rows; the list-owned dialog must preserve its typed draft.
    await row.getByRole("button", { name: "Edit" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(row.getByRole("button", { name: "Edit" })).toBeDisabled();
    await page.getByLabel(/Replace connection config/).check();
    await fillConfig(page, target, "definitely-wrong-password");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByText(/auth-failed/)).toBeVisible();

    await expect(page.getByLabel("Host")).toHaveValue(target.host);
    await expect(page.getByLabel("Database")).toHaveValue(target.database);
    await page.getByLabel("Password", { exact: true }).fill(target.password);
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByRole("region", { name: "Edit connection", exact: true })).toHaveCount(0);
    await expect(row.getByText("active")).toBeVisible();


    await page.getByRole("button", { name: "New connection" }).click();
    await expect(page.getByLabel("Display name")).toBeFocused();
    await page.getByLabel("Display name").fill("Broken");
    await fillConfig(page, target, "definitely-wrong-password");
    await page.getByRole("button", { name: "Test connection" }).click();
    await expect(page.getByText("Connection test failed: auth-failed.")).toBeVisible();
    await expect(page.getByText(/definitely-wrong-password/)).toHaveCount(0);
    await page.getByRole("button", { name: "Create" }).click();
    await expect(page.getByText("connection test failed: auth-failed")).toBeVisible();

    await page.getByRole("button", { name: "Dismiss" }).click();


    await row.getByRole("button", { name: "Archive" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(1);
    await expect(page.getByText(/permanently discards the stored/)).toBeVisible();
    await page.getByRole("button", { name: "Archive connection" }).click();
    await expect(row.getByText("archived")).toBeVisible();
    await expect(row.getByRole("button", { name: "Test" })).toHaveCount(0);
    await expect(row.getByRole("button", { name: "Archive" })).toHaveCount(0);
    await expect(row.getByRole("button", { name: "Edit" })).toBeVisible();

    // Verify client caches reset on session changes; cross-user server denial is covered by Go API E2E until browser user provisioning exists.
    await page.goto("/");
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/login$/);
    // Use the bounded-retry sign-in: the per-account-and-client bucket can still be refilling from this file's earlier logins.
    await signInForScenario(page, email, password);
    await page.getByRole("link", { name: /Connections/ }).click();
    await expect(
      page.getByRole("row", { name: /Primary \(renamed\)/ }).getByText("archived"),
    ).toBeVisible();
  });
});
