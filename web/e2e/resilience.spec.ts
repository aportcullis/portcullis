import { expect, test } from "@playwright/test";

import { signInForScenario } from "@e2e/login";
import { failProcedure, fulfillEmptyMessage } from "@e2e/rpc";

const email = "admin@example.com";
const password = "correct-horse-battery";

test.describe("application resilience", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/login");
    await signInForScenario(page, email, password);
  });

  test("unknown addresses render a not-found page inside the application frame", async ({ page }) => {
    for (const path of ["/no-such-page", "/requests/abc/result/extra", "/connections/unknown/child"]) {
      await page.goto(path);
      await expect(page.getByRole("heading", { name: "Page not found" })).toBeVisible();
      await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
    }
    await page.getByRole("link", { name: "Go to start page" }).click();
    await expect(page).toHaveURL(/\/connections$/);
    for (const path of ["/requests", "/requests/new", "/connections"]) {
      await page.goto(path);
      await expect(page.getByRole("heading", { name: "Page not found" })).toHaveCount(0);
    }
  });

  test("request pages keep working when the instance config cannot be read", async ({ page }) => {
    const restore = await failProcedure(page, "Auth/GetConfig", "unavailable");
    await page.goto("/requests/new");
    await expect(page.getByLabel("Title", { exact: true })).toBeVisible();
    await expect(page.getByText(/\/ \d+ characters/)).toHaveCount(0);
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByRole("button", { name: "Submit", exact: true }).click();
    await expect(page.getByText("Enter a request title.")).toBeVisible();

    await page.goto("/requests");
    await page.getByRole("link", { name: "Details" }).first().click();
    await expect(page.getByRole("heading", { name: "Request evidence" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Something went wrong" })).toHaveCount(0);

    await page.goto("/requests/new");
    await restore();
    await expect(page.getByText(/\/ \d+ characters/).first()).toBeVisible({ timeout: 15_000 });
  });

  test("a failed background list refresh keeps the last rows and says so", async ({ page }) => {
    await page.goto("/requests");
    const rows = page.getByRole("row", { name: /ReqTarget/ });
    await expect(rows.first()).toBeVisible();
    const shownRows = await rows.count();

    const restoreUnavailable = await failProcedure(page, "AccessRequests/List", "unavailable");
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect(page.getByText(/Refresh failed: simulated failure Showing the last loaded requests\./)).toBeVisible();
    await expect(rows).toHaveCount(shownRows);
    await restoreUnavailable();

    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect(page.getByText(/Refresh failed/)).toHaveCount(0);
    await expect(rows).toHaveCount(shownRows);

    const restoreDenied = await failProcedure(page, "AccessRequests/List", "permission_denied");
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect(rows).toHaveCount(0);
    await expect(page.getByText(/Refresh failed/)).toHaveCount(0);
    await restoreDenied();
  });

  test("a failed or empty policy read shows an error with a working retry", async ({ page }) => {
    const row = page.getByRole("row", { name: /ReqTarget/ });
    const restore = await failProcedure(page, "ConnectionPolicies/Get", "unavailable");
    await row.getByRole("button", { name: "Policy" }).click();
    await expect(page.getByText("simulated failure")).toBeVisible();
    await expect(page.getByRole("button", { name: "Save policy" })).toHaveCount(0);
    await restore();
    await page.getByRole("button", { name: "Retry" }).click();
    await expect(page.getByLabel("Read approvals")).toBeVisible();
    await expect(page.getByText("simulated failure")).toHaveCount(0);
    await page.getByRole("button", { name: "Dismiss" }).click();

    await page.route("**/portcullis.v1.ConnectionPolicies/Get", fulfillEmptyMessage);
    await row.getByRole("button", { name: "Policy" }).click();
    await expect(page.getByText("The server returned no policy for this connection.")).toBeVisible();
    await expect(page.getByRole("button", { name: "Save policy" })).toHaveCount(0);
    await page.unroute("**/portcullis.v1.ConnectionPolicies/Get", fulfillEmptyMessage);
    await page.getByRole("button", { name: "Retry" }).click();
    await expect(page.getByRole("button", { name: "Save policy" })).toBeVisible();
  });
});
