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


    const target = await loadTarget();
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
    await page.getByLabel("SQL", { exact: true }).fill("select 1");
    await page.getByRole("button", { name: "Submit" }).click();
    const approvedRow = page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Approved" });
    await expect(page.getByText("Approved", { exact: true })).toBeVisible();
    await page.getByRole("link", { name: "Back to requests", exact: true }).click();
    await expect(approvedRow).toBeVisible();


    // A write is refused by the read-only policy. Create always persists a draft; only the subsequent Submit is refused — so the error shows AND a Draft row is left behind (the payload can then be fixed or cancelled).
    await page.getByRole("button", { name: "New request" }).click();
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("SQL", { exact: true }).fill("update t set x = 1");
    await page.getByRole("button", { name: "Submit" }).click();
    await expect(page.getByText(/not allowed/)).toBeVisible();
    await page.getByRole("link", { name: "Back to requests", exact: true }).click();

    // That refused submit left its draft. The draft must be RECOVERABLE from the detail view — fix the statement, submit, and it goes through (ADR-0018:100). Without a Submit there the draft is saved, editable, and undeliverable.
    const draftRow = page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" });
    await expect(draftRow).toBeVisible();
    await draftRow.getByRole("link", { name: "Details" }).click();
    await page.getByRole("button", { name: "Edit draft" }).click();
    // A server payload rejection must preserve typed SQL so the draft remains editable.
    await page.getByLabel("Auto-format SQL").uncheck();
    const overBudget = `select 1 -- ${"x".repeat(57 * 1024)}`;
    await page.getByLabel("SQL", { exact: true }).fill(overBudget);
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page.getByRole("main").getByText(/too large|payload/i)).toBeVisible();
    await expect(page.getByRole("main").getByLabel("SQL", { exact: true })).toHaveValue(overBudget);

    await page.getByLabel("SQL", { exact: true }).fill("select 2");

    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page.getByRole("button", { name: "Edit draft" })).toBeVisible();
    await page.getByRole("main").getByRole("button", { name: "Submit" }).click();
    await expect(page.getByText("Approved", { exact: true })).toBeVisible();
    await page.getByRole("link", { name: "Back to requests", exact: true }).click();
    await expect(
      page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" }),
    ).toHaveCount(0);


    await page.getByRole("button", { name: "New request" }).click();
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("SQL", { exact: true }).fill("select 3");
    await page.getByRole("button", { name: "Save draft" }).click();
    await page.getByRole("link", { name: "Back to requests", exact: true }).click();
    const savedRow = page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" });
    await expect(savedRow).toBeVisible();
    await savedRow.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(
      page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Cancelled" }),
    ).toBeVisible();


    await page.getByRole("button", { name: "New request" }).click();
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("SQL", { exact: true }).fill("select 4");
    await page.getByRole("button", { name: "Save draft" }).click();
    await page.getByRole("link", { name: "Back to requests", exact: true }).click();
    const sendableRow = page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" });
    await expect(sendableRow).toBeVisible();
    await sendableRow.getByRole("button", { name: "Submit", exact: true }).click();
    await expect(
      page.getByRole("row", { name: /ReqTarget/ }).filter({ hasText: "Draft" }),
    ).toHaveCount(0);
  });
  test("long SQL stays scrollable without pushing request actions outside the page", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL(/\/connections$/);
    await page.getByRole("link", { name: /Requests/ }).click();
    await page.getByRole("button", { name: "New request" }).click();
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("Auto-format SQL").uncheck();
    const sql = `SELECT 2 -- ${"long-sql-".repeat(100)}`;
    await page.getByLabel("SQL", { exact: true }).fill(sql);
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    const dialog = page.getByRole("region", { name: "Request details" });
    const submit = dialog.getByRole("button", { name: "Submit", exact: true });
    await expect(submit).toBeVisible();
    const bounds = await dialog.boundingBox();
    const action = await submit.boundingBox();
    expect(bounds).not.toBeNull();
    expect(action).not.toBeNull();
    if (!bounds || !action) throw new Error("Request page and submit action must be laid out");
    expect(action.x).toBeGreaterThanOrEqual(bounds.x);
    expect(action.x + action.width).toBeLessThanOrEqual(bounds.x + bounds.width);
    await expect(dialog.locator("pre")).toHaveText(sql);
  });
  test("request composition and details use reloadable pages with browser history", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL(/\/connections$/);
    await page.goto("/requests");
    await page.getByRole("button", { name: "New request" }).click();
    await expect(page).toHaveURL(/\/requests\/new$/);
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.reload();
    await expect(page.getByRole("heading", { name: "New access request" })).toBeVisible();
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("Auto-format SQL").uncheck();
    const sql = "SELECT 42 AS page_request";
    await page.getByLabel("SQL", { exact: true }).fill(sql);
    const editor = await page.getByLabel("SQL", { exact: true }).boundingBox();
    expect(editor?.height).toBeGreaterThanOrEqual(320);
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page).toHaveURL(/\/requests\/[0-9a-f-]+$/);
    const detailURL = page.url();
    await page.reload();
    await expect(page.locator("pre")).toHaveText(sql);
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.getByRole("link", { name: "Back to requests", exact: true }).click();
    await expect(page).toHaveURL(/\/requests$/);
    await page.goBack();
    await expect(page).toHaveURL(detailURL);
    await expect(page.locator("pre")).toHaveText(sql);
  });
  test("SQL formatting is automatic, reversible and saved only through explicit draft actions", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL(/\/connections$/);
    await page.goto("/requests/new");
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    const original = "select 42 as answer from (select 1) as source where 1 = 1";
    const editor = page.getByLabel("SQL", { exact: true });
    await editor.fill(original);
    await editor.press("Shift+Tab");
    await expect(editor).toHaveValue(/select\n {2}42 as answer/);
    const formatted = await editor.inputValue();
    await page.getByRole("button", { name: "Undo formatting", exact: true }).click();
    await expect(editor).toHaveValue(original);
    await expect(page.getByLabel("Auto-format SQL")).not.toBeChecked();
    await editor.focus();
    await editor.press("Shift+Tab");
    await expect(editor).toHaveValue(original);
    await page.getByRole("button", { name: "Format SQL", exact: true }).click();
    await expect(editor).toHaveValue(formatted);
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    await expect(page).toHaveURL(/\/requests\/[0-9a-f-]+$/);
    await expect(page.locator("pre")).toHaveText(formatted);
    await page.reload();
    await expect(page.locator("pre")).toHaveText(formatted);
    await page.getByRole("button", { name: "Edit draft", exact: true }).click();
    const invalid = "select 'unfinished";
    await editor.fill(invalid);
    await editor.press("Shift+Tab");
    await expect(editor).toHaveValue(invalid);
    await expect(page.getByText("SQL could not be formatted. Your input is unchanged.")).toBeVisible();
    await expect(page.getByRole("dialog")).toHaveCount(0);
  });

});
