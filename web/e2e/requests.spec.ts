import { expect, test } from "@playwright/test";

import { loadTarget } from "@e2e/target";


test.describe.serial("access requests", () => {
  const email = "admin@example.com";
  const password = "correct-horse-battery";

  test("auto-approve, disallowed class leaves a draft, then cancel", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL(/\/connections$/);


    const target = loadTarget();
    await page.getByRole("button", { name: "New connection" }).click();
    await page.getByLabel("Display name").fill("ReqTarget");
    await page.getByLabel("Host").fill(target.host);
    await page.getByLabel("Port").fill(String(target.port));
    await page.getByLabel("Database").fill(target.database);
    await page.getByLabel("User").fill(target.user);
    await page.getByLabel("Password", { exact: true }).fill(target.password);
    await page.getByLabel("TLS mode").selectOption("disable");
    await page.getByRole("button", { name: "Create" }).click();
    const connRow = page.getByRole("row", { name: /ReqTarget/ });
    await expect(connRow).toBeVisible();

    await connRow.getByRole("button", { name: "Policy" }).click();
    await page.getByLabel("Read approvals").fill("0");
    await expect(page.getByText(/auto-approved by the system/)).toBeVisible();
    await page.getByRole("button", { name: "Save policy" }).click();
    await expect(page.getByRole("button", { name: "Save policy" })).toHaveCount(0);


    await page.getByRole("link", { name: "Requests" }).click();
    await expect(page).toHaveURL(/\/requests$/);


    await page.getByRole("button", { name: "New request" }).click();
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("SQL").fill("select 1");
    await page.getByRole("button", { name: "Submit" }).click();
    const approvedRow = page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Approved" });
    await expect(approvedRow).toBeVisible();


    // A write is refused by the read-only policy. Create always persists a draft; only the subsequent Submit is refused — so the error shows AND a Draft row is left behind (the payload can then be fixed or cancelled).
    await page.getByRole("button", { name: "New request" }).click();
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("SQL").fill("update t set x = 1");
    await page.getByRole("button", { name: "Submit" }).click();
    await expect(page.getByText(/not allowed/)).toBeVisible();
    await page.keyboard.press("Escape");

    // That refused submit left its draft. The draft must be RECOVERABLE from the detail view — fix the statement, submit, and it goes through (ADR-0018:100). Without a Submit there the draft is saved, editable, and undeliverable.
    const draftRow = page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" });
    await expect(draftRow).toBeVisible();
    await draftRow.getByRole("button", { name: "Details" }).click();
    await page.getByRole("button", { name: "Edit draft" }).click();
    // A server payload rejection must preserve typed SQL so the draft remains editable.
    const overBudget = `select 1 -- ${"x".repeat(57 * 1024)}`;
    await page.getByLabel("SQL").fill(overBudget);
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page.getByRole("dialog").getByText(/too large|payload/i)).toBeVisible();
    await expect(page.getByRole("dialog").getByLabel("SQL")).toHaveValue(overBudget);

    await page.getByLabel("SQL").fill("select 2");

    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await draftRow.getByRole("button", { name: "Details" }).click();

    await page.getByRole("dialog").getByRole("button", { name: "Submit" }).click();
    await expect(
      page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" }),
    ).toHaveCount(0);


    await page.getByRole("button", { name: "New request" }).click();
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("SQL").fill("select 3");
    await page.getByRole("button", { name: "Save draft" }).click();
    const savedRow = page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" });
    await expect(savedRow).toBeVisible();
    await savedRow.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(
      page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Cancelled" }),
    ).toBeVisible();


    await page.getByRole("button", { name: "New request" }).click();
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("SQL").fill("select 4");
    await page.getByRole("button", { name: "Save draft" }).click();
    const sendableRow = page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" });
    await expect(sendableRow).toBeVisible();
    await sendableRow.getByRole("button", { name: "Submit", exact: true }).click();
    await expect(
      page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" }),
    ).toHaveCount(0);
  });
  test("long SQL stays scrollable without pushing request actions out of the dialog", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL(/\/connections$/);
    await page.getByRole("link", { name: /Requests/ }).click();
    await page.getByRole("button", { name: "New request" }).click();
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    const sql = `SELECT 2 -- ${"long-sql-".repeat(100)}`;
    await page.getByLabel("SQL").fill(sql);
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    const row = page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" });
    await row.getByRole("button", { name: "Details", exact: true }).click();
    const dialog = page.getByRole("dialog");
    const submit = dialog.getByRole("button", { name: "Submit", exact: true });
    await expect(submit).toBeVisible();
    const bounds = await dialog.boundingBox();
    const action = await submit.boundingBox();
    expect(bounds).not.toBeNull();
    expect(action).not.toBeNull();
    if (!bounds || !action) throw new Error("Request dialog and submit action must be laid out");
    expect(action.x).toBeGreaterThanOrEqual(bounds.x);
    expect(action.x + action.width).toBeLessThanOrEqual(bounds.x + bounds.width);
    await expect(dialog.locator("pre")).toHaveText(sql);
  });
});
