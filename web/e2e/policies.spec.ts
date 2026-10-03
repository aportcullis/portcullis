import { expect, test } from "@playwright/test";

import { loadTarget } from "@e2e/target";


test.describe.serial("connection policies", () => {
  const email = "admin@example.com";
  const password = "correct-horse-battery";

  test("defaults → enable write with warning → new version → archive freeze", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL(/\/connections$/);


    const target = await loadTarget();
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


    await row.getByRole("button", { name: "Policy" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByLabel("Read approvals")).toHaveValue("1");
    await expect(page.getByRole("checkbox", { name: "Allow Read" })).toBeChecked();
    await expect(page.getByRole("checkbox", { name: "Allow Write" })).not.toBeChecked();
    await expect(page.getByRole("checkbox", { name: "Allow DDL" })).not.toBeChecked();
    await expect(page.getByLabel("Timeout (s)")).toHaveValue("30");
    await expect(page.getByLabel("Max rows")).toHaveValue("10000");


    await page.getByRole("checkbox", { name: "Allow Write" }).check();
    await expect(page.getByText(/Enabling write allows statements/)).toBeVisible();


    await page.getByLabel("Read approvals").fill("0");
    await expect(page.getByText(/auto-approved by the system/)).toBeVisible();

    await page.getByLabel("Write approvals").fill("2");
    await page.getByRole("button", { name: "Save policy" }).click();
    await expect(page.getByRole("button", { name: "Save policy" })).toHaveCount(0);


    await row.getByRole("button", { name: "Policy" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByRole("checkbox", { name: "Allow Write" })).toBeChecked();
    await expect(page.getByLabel("Write approvals")).toHaveValue("2");
    await expect(page.getByLabel("Read approvals")).toHaveValue("0");
    await page.getByRole("button", { name: "Dismiss" }).click();


    await row.getByRole("button", { name: "Policy" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.getByLabel("Timeout (s)").fill("0");
    await page.getByRole("button", { name: "Save policy" }).click();
    await expect(page.getByText(/Query timeout must be between/)).toBeVisible();
    await page.getByRole("button", { name: "Dismiss" }).click();


    await row.getByRole("button", { name: "Archive" }).click();
    await page.getByRole("button", { name: "Archive connection" }).click();
    await expect(row.getByText("archived")).toBeVisible();
    await expect(row.getByRole("button", { name: "Policy" })).toHaveCount(0);
  });
});
