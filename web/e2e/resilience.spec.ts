import { expect, test } from "@playwright/test";

import { signInForScenario } from "@e2e/login";
import { failProcedure } from "@e2e/rpc";

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
});
