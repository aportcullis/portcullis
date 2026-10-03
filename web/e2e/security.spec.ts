import { expect, test } from "@playwright/test";

test("embedded SPA works while inline and foreign scripts are refused", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: /^(Create admin account|Sign in)$/ })).toBeVisible();
  const inlineExecuted = await page.evaluate(() => {
    const script = document.createElement("script");
    script.textContent = "document.documentElement.dataset.cspInlineProbe = 'executed'";
    document.head.append(script);
    return document.documentElement.dataset.cspInlineProbe === "executed";
  });
  expect(inlineExecuted).toBe(false);

  const violation = await page.evaluate(() => new Promise<{ directive: string; uri: string }>(resolve => {
    document.addEventListener("securitypolicyviolation", event => {
      if (event.blockedURI.includes("portcullis-csp.invalid")) {
        resolve({ directive: event.effectiveDirective, uri: event.blockedURI });
      }
    });
    const script = document.createElement("script");
    script.src = "https://portcullis-csp.invalid/probe.js";
    document.head.append(script);
  }));
  expect(violation.directive).toMatch(/^script-src(?:-elem)?$/);
  expect(violation.uri).toContain("portcullis-csp.invalid");
});
