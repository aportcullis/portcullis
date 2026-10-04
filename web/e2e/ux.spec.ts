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
    if (box === null) throw new Error(`${name} has no layout box`);
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(320);
  }
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.screenshot({ path: "../.test-docker/readme-media/narrow-light.png", fullPage: true });
  await page.emulateMedia({ colorScheme: "dark" });
  await page.evaluate(() => document.documentElement.classList.add("dark"));
  await expect(page.getByRole("img", { name: "Portcullis" })).toHaveAttribute("src", "/brand/logo-dark.svg");
  const foreground = await page.locator("body").evaluate(element => getComputedStyle(element).color);
  await expect.poll(() => page.getByRole("button", { name: "Sign out", exact: true }).evaluate(element => getComputedStyle(element).color)).toBe(foreground);
  await page.screenshot({ path: "../.test-docker/readme-media/narrow-dark.png", fullPage: true });
});

// Hold a real response to distinguish pending data from empty or failed data.
test("request loading is bounded and expands the actual workflow inline after data arrives", async ({ page }) => {
  await page.goto("/login");
  await signInForScenario(page, "admin@example.com", "correct-horse-battery");
  let release = (): void => {};
  const pending = new Promise<void>(resolve => { release = resolve; });
  await page.route("**/portcullis.v1.AccessRequests/List", async route => {
    await pending;
    await route.continue();
  });
  try {
    await page.goto("/requests");
    const loading = page.getByRole("status").filter({ hasText: "Loading requests…" });
    await expect(loading).toHaveAttribute("aria-busy", "true");
    await expect(loading.locator('[aria-hidden="true"] > div')).toHaveCount(3);
    await expect(page.getByText("No access requests yet.")).toHaveCount(0);
    release();
    await expect(loading).toHaveCount(0);
    const disclosure = page.getByRole("button", { name: "Query review", exact: true }).first();
    await disclosure.focus();
    await disclosure.press("Enter");
    await expect(disclosure).toHaveAttribute("aria-expanded", "true");
    const workflow = page.getByRole("region", { name: "Workflow for Query review" });
    await expect(workflow.getByRole("list", { name: "Request stages" }).getByRole("listitem")).toHaveCount(4);
    await expect(workflow).toContainText("Execution");
    await disclosure.press("Enter");
    await expect(workflow).toHaveCount(0);
    await expect(page.getByRole("dialog")).toHaveCount(0);
  } finally { release(); }
});

// Type parameters key by key, as a keyboard user does, so a re-rendered row cannot drop focus mid-word.
test("parameter fields keep focus and every keystroke while typing", async ({ page }) => {
  await page.goto("/login");
  await signInForScenario(page, "admin@example.com", "correct-horse-battery");
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Requests" }).click();
  await page.getByRole("button", { name: "New request" }).click();
  await page.getByRole("button", { name: "Add parameter" }).click();
  for (const [label, text] of [["Name", "customer_id"], ["Value", "42"]]) {
    const field = page.getByRole("main").getByLabel(label, { exact: true });
    await field.click();
    await field.pressSequentially(text);
    await expect(field).toHaveValue(text);
    await expect(field).toBeFocused();
  }
});
