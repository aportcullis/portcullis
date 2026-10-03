import { expect, test } from "@playwright/test";
import { signInForScenario } from "@e2e/login";

// Exercise navigation and request composition without creating or submitting SQL.
test("narrow screens keep navigation and request actions reachable with a current-location cue", async ({ page }) => {
  await page.goto("/login");
  await signInForScenario(page, "admin@example.com", "correct-horse-battery");
  await page.setViewportSize({ width: 320, height: 800 });
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Connections" })).toHaveAttribute("aria-current", "page");
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Requests" }).click();
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Requests" })).toHaveAttribute("aria-current", "page");
  await page.getByRole("button", { name: "New request" }).click();
  await expect(page.getByLabel("Title", { exact: true })).toBeVisible();
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
  for (const name of ["Save draft", "Submit"]) {
    const button = page.getByRole("button", { name, exact: true });
    await button.scrollIntoViewIfNeeded();
    const box = await button.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(320);
  }
  await expect(page.getByRole("dialog")).toHaveCount(0);
});
