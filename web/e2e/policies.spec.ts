import { expect, test } from "@playwright/test";

import { loadTarget } from "./target";

// The connection-policy vertical in a real browser (ADR-0015): the v1 default
// policy a new connection is born with, enabling write behind a destructive
// warning, the auto-approve hint, versioning across saves, and the archive
// freeze. Runs AFTER connections.spec.ts (alphabetical) reusing its admin, and
// BEFORE zz-auth-lockout.spec.ts, which locks that account.
test.describe.serial("connection policies", () => {
  const email = "admin@example.com";
  const password = "correct-horse-battery";

  test("defaults → enable write with warning → new version → archive freeze", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL(/\/connections$/);

    // A fresh connection to own a policy (the connections.spec.ts row is
    // archived by now, and archived rows have no policy editor).
    const target = loadTarget();
    await page.getByRole("button", { name: "New connection" }).click();
    await page.getByLabel("Display name").fill("Gated");
    await page.getByLabel("Host").fill(target.host);
    await page.getByLabel("Port").fill(String(target.port));
    await page.getByLabel("Database").fill(target.database);
    await page.getByLabel("User").fill(target.user);
    await page.getByLabel("Password", { exact: true }).fill(target.password);
    await page.getByLabel("TLS mode").selectOption("disable");
    await page.getByRole("button", { name: "Create" }).click();
    const row = page.getByRole("row", { name: /Gated/ });
    await expect(row).toBeVisible();

    // The connection is born with the read-only v1 defaults (ADR-0015).
    await row.getByRole("button", { name: "Policy" }).click();
    await expect(page.getByLabel("Read approvals")).toHaveValue("1");
    await expect(page.getByRole("checkbox", { name: "Allow Read" })).toBeChecked();
    await expect(page.getByRole("checkbox", { name: "Allow Write" })).not.toBeChecked();
    await expect(page.getByRole("checkbox", { name: "Allow DDL" })).not.toBeChecked();
    await expect(page.getByLabel("Timeout (s)")).toHaveValue("30");
    await expect(page.getByLabel("Max rows")).toHaveValue("10000");

    // Enabling write surfaces the destructive warning before anything is saved.
    await page.getByRole("checkbox", { name: "Allow Write" }).check();
    await expect(page.getByText(/Enabling write allows statements/)).toBeVisible();

    // A zero quorum is allowed but called out (system auto-approval, §4.3).
    await page.getByLabel("Read approvals").fill("0");
    await expect(page.getByText(/auto-approved by the system/)).toBeVisible();

    await page.getByLabel("Write approvals").fill("2");
    await page.getByRole("button", { name: "Save policy" }).click();
    await expect(page.getByRole("button", { name: "Save policy" })).toHaveCount(0);

    // Reopening shows the NEW current version's values — the save appended a
    // version and moved the pointer.
    await row.getByRole("button", { name: "Policy" }).click();
    await expect(page.getByRole("checkbox", { name: "Allow Write" })).toBeChecked();
    await expect(page.getByLabel("Write approvals")).toHaveValue("2");
    await expect(page.getByLabel("Read approvals")).toHaveValue("0");
    await page.getByRole("button", { name: "Dismiss" }).click();

    // Out-of-bounds limits are rejected inline (mirrors the server bounds).
    await row.getByRole("button", { name: "Policy" }).click();
    await page.getByLabel("Timeout (s)").fill("0");
    await page.getByRole("button", { name: "Save policy" }).click();
    await expect(page.getByText(/Query timeout must be between/)).toBeVisible();
    await page.getByRole("button", { name: "Dismiss" }).click();

    // Archive freezes the policy: the editor affordance disappears with the row.
    await row.getByRole("button", { name: "Archive" }).click();
    await page.getByRole("button", { name: "Archive connection" }).click();
    await expect(row.getByText("archived")).toBeVisible();
    await expect(row.getByRole("button", { name: "Policy" })).toHaveCount(0);
  });
});
