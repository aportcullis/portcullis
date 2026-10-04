import { expect, test } from "@playwright/test";

import { signInForScenario } from "@e2e/login";
import { failProcedure, fulfillEmptyMessage } from "@e2e/rpc";

const email = "admin@example.com";
const password = "correct-horse-battery";

test.describe("sign-in return path", () => {
  test("a deep link survives the sign-in redirect", async ({ page }) => {
    for (const [deepLink, landing] of [
      ["/requests/new", /\/requests\/new$/],
      ["/requests?from=link#top", /\/requests\?from=link#top$/],
      ["/no-such-page", /\/no-such-page$/],
    ] as const) {
      await page.context().clearCookies();
      await page.goto(deepLink);
      await expect(page).toHaveURL(/\/login\?returnTo=/);
      await signInForScenario(page, email, password, landing);
    }
  });

  test("a crafted return path never leaves the application", async ({ page, baseURL }) => {
    const applicationHost = new URL(baseURL ?? "http://127.0.0.1:18080").host;
    for (const crafted of ["//evil.example/requests", "/\\evil.example/requests", "https://evil.example/", "javascript:alert(1)", "/\t/evil.example/requests", "/login?returnTo=%2Frequests", "/.//evil.example/requests", "/%2e//evil.example/requests"]) {
      await page.context().clearCookies();
      await page.goto(`/login?returnTo=${encodeURIComponent(crafted)}`);
      await signInForScenario(page, email, password);
      expect(new URL(page.url()).host).toBe(applicationHost);
    }
  });

  test("an explicit sign-out does not carry the page that was open", async ({ page }) => {
    await page.goto("/login");
    await signInForScenario(page, email, password);
    await page.goto("/requests");
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(/\/login$/);
  });
});

test("a pending session check shows a loading status instead of a blank page", async ({ page }) => {
  await page.goto("/login");
  await signInForScenario(page, email, password);
  let release = () => {};
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/portcullis.v1.Auth/Me", async (route) => {
    await held;
    await route.continue();
  });
  try {
    await page.goto("/requests/new");
    const loading = page.getByRole("status").filter({ hasText: "Loading your session…" });
    await expect(loading).toHaveAttribute("aria-busy", "true");
    await expect(page.getByRole("navigation", { name: "Main" })).toHaveCount(0);
    await expect(page.getByLabel("Title", { exact: true })).toHaveCount(0);
    await expect(page).toHaveURL(/\/requests\/new$/);
    release();
    await expect(page.getByLabel("Title", { exact: true })).toBeVisible();
    await expect(page.getByRole("status").filter({ hasText: "Loading your session…" })).toHaveCount(0);
    await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
  } finally {
    release();
    await page.unroute("**/portcullis.v1.Auth/Me");
  }
});

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

  test("a refused Execute keeps its row error through the list refresh it triggers", async ({ page }) => {
    await page.goto("/requests/new");
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("Title", { exact: true }).fill("Row state check");
    await page.getByLabel("SQL", { exact: true }).fill("select 2");
    await page.getByRole("button", { name: "Submit", exact: true }).click();
    await expect(page.getByText("Approved", { exact: true })).toBeVisible();

    await page.goto("/requests");
    const row = page.getByRole("row", { name: /Row state check/ });
    await expect(row.getByRole("button", { name: "Execute" })).toBeVisible();
    const listRefreshed = page.waitForResponse((response) => response.url().endsWith("/portcullis.v1.AccessRequests/List"));
    const restore = await failProcedure(page, "QueryExecutions/Execute", "unavailable");
    await row.getByRole("button", { name: "Execute" }).click();
    await listRefreshed;
    await expect(row.getByRole("alert")).toHaveText("simulated failure");
    await expect(row.getByRole("button", { name: "Execute" })).toBeEnabled();
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect(row.getByRole("alert")).toHaveText("simulated failure");
    await restore();
  });

  test("changing the result page keeps the shown rows until the next page arrives", async ({ page }) => {
    await page.goto("/requests/new");
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("Title", { exact: true }).fill("Result paging check");
    await page.getByLabel("SQL", { exact: true }).fill("select 1000 + g as marker from generate_series(1, 25) as g order by g");
    await page.getByRole("button", { name: "Submit", exact: true }).click();
    const details = page.getByRole("region", { name: "Request details" });
    await details.getByRole("button", { name: "Execute", exact: true }).click();
    await details.getByRole("link", { name: "Result", exact: true }).click();
    const results = page.getByRole("region", { name: "Query results" });
    await expect(results.getByText("1001", { exact: true })).toBeVisible();

    let release = () => {};
    const held = new Promise<void>((resolve) => { release = resolve; });
    await page.route("**/portcullis.v1.QueryExecutions/GetResult", async (route) => {
      await held;
      await route.continue();
    });
    try {
      await results.getByRole("button", { name: "Next page", exact: true }).click();
      await expect(results.getByRole("status").filter({ hasText: "Updating results…" })).toBeVisible();
      await expect(results.getByText("1001", { exact: true })).toBeVisible();
      await expect(results.getByRole("status").filter({ hasText: "Loading result…" })).toHaveCount(0);
      await expect(results.getByRole("button", { name: "Next page", exact: true })).toBeDisabled();
      release();
      await expect(results.getByText("1021", { exact: true })).toBeVisible();
      await expect(results.getByText("1001", { exact: true })).toHaveCount(0);
      await expect(results.getByRole("status").filter({ hasText: "Updating results…" })).toHaveCount(0);
    } finally {
      release();
      await page.unroute("**/portcullis.v1.QueryExecutions/GetResult");
    }
  });

  test("a failed background detail refresh keeps the details and says so", async ({ page }) => {
    await page.goto("/requests");
    await page.getByRole("row", { name: /Row state check/ }).getByRole("link", { name: "Details" }).click();
    await expect(page.getByRole("heading", { name: "Request evidence" })).toBeVisible();
    const detailURL = page.url();

    const restore = await failProcedure(page, "AccessRequests/Get", "unavailable");
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect(page.getByText(/Refresh failed: simulated failure Showing the last loaded details\./)).toBeVisible();
    await expect(page.getByRole("heading", { name: "Request evidence" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Review and execution" })).toBeVisible();

    await page.goto(detailURL);
    await expect(page.getByText("simulated failure", { exact: true })).toBeVisible();
    await expect(page.getByText(/Refresh failed/)).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "Request evidence" })).toHaveCount(0);

    await restore();
    await page.reload();
    await expect(page.getByRole("heading", { name: "Request evidence" })).toBeVisible();
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect(page.getByText(/Refresh failed/)).toHaveCount(0);
  });

  test("the request state filter shows human labels and filters by the server value", async ({ page }) => {
    await page.goto("/requests");
    const filter = page.getByLabel("Filter", { exact: true });
    await expect(filter.locator("option")).toHaveText([
      "All states", "Draft", "Pending", "Approved", "Executing", "Succeeded", "Failed", "Outcome unknown", "Rejected", "Expired", "Cancelled",
    ]);
    const listed = page.waitForRequest((request) => request.url().endsWith("/portcullis.v1.AccessRequests/List"));
    await filter.selectOption({ label: "Approved" });
    await listed;
    await expect(filter).toHaveValue("approved");
    await expect(page.getByRole("row", { name: /Row state check/ })).toBeVisible();
  });

  test("closing an inline panel returns focus to the control that opened it", async ({ page }) => {
    const policyButton = page.getByRole("row", { name: /ReqTarget/ }).getByRole("button", { name: "Policy" });
    await policyButton.click();
    // The policy form renders after its read resolves; the panel then focuses its first field.
    await expect(page.getByRole("checkbox", { name: "Allow Read" })).toBeFocused();
    await page.getByRole("button", { name: "Dismiss" }).click();
    await expect(policyButton).toBeFocused();

    await policyButton.click();
    await expect(page.getByRole("button", { name: "Save policy" })).toBeVisible();
    await page.getByRole("button", { name: "Save policy" }).click();
    await expect(page.getByRole("button", { name: "Save policy" })).toHaveCount(0);
    await expect(policyButton).toBeFocused();

    const newConnection = page.getByRole("button", { name: "New connection" });
    await newConnection.click();
    await expect(page.getByLabel("Display name")).toBeFocused();
    await page.getByRole("button", { name: "Dismiss" }).click();
    await expect(newConnection).toBeFocused();
  });

  test("a typed replacement password does not survive closing the edit panel", async ({ page }) => {
    const row = page.getByRole("row", { name: /ReqTarget/ });
    const replace = page.getByRole("checkbox", { name: /Replace connection config/ });
    await row.getByRole("button", { name: "Edit" }).click();
    await replace.check();
    await page.getByLabel("Password", { exact: true }).fill("typed-replacement-secret");
    await page.getByRole("button", { name: "Dismiss" }).click();
    await expect(page.getByLabel("Password", { exact: true })).toHaveCount(0);

    await row.getByRole("button", { name: "Edit" }).click();
    await expect(replace).not.toBeChecked();
    await replace.check();
    await expect(page.getByLabel("Password", { exact: true })).toHaveValue("");
    await page.getByRole("button", { name: "Dismiss" }).click();
  });

  test("an invalid max result size is reported instead of silently ignored", async ({ page }) => {
    const policyButton = page.getByRole("row", { name: /ReqTarget/ }).getByRole("button", { name: "Policy" });
    const maxResult = page.getByLabel("Max result (MiB)");
    const save = page.getByRole("button", { name: "Save policy" });
    await policyButton.click();
    const original = await maxResult.inputValue();

    for (const [raw, message] of [
      ["1.5", "Max result must be a positive whole number of MiB."],
      ["0", "Max result must be a positive whole number of MiB."],
      ["", "Enter the max result size in MiB."],
    ]) {
      await maxResult.fill(raw);
      await save.click();
      await expect(page.getByText(message)).toBeVisible();
      await expect(maxResult).toHaveAttribute("aria-invalid", "true");
      await expect(save).toBeVisible();
    }

    await maxResult.fill("8");
    await expect(maxResult).toHaveAttribute("aria-invalid", "false");
    await save.click();
    await expect(save).toHaveCount(0);
    await policyButton.click();
    await expect(maxResult).toHaveValue("8");
    await maxResult.fill(original);
    await save.click();
    await expect(save).toHaveCount(0);
  });

  test("a CSV export survives changing the shown page, sort or filter while it streams", async ({ page }) => {
    // This scenario creates its own executed request so it runs alone as well as in sequence.
    await page.goto("/requests/new");
    await page.getByLabel("Connection").selectOption({ label: "ReqTarget" });
    await page.getByLabel("Title", { exact: true }).fill(`Export view change ${Date.now()}`);
    await page.getByLabel("SQL", { exact: true }).fill("select 1000 + g as marker from generate_series(1, 25) as g order by g");
    await page.getByRole("button", { name: "Submit", exact: true }).click();
    const details = page.getByRole("region", { name: "Request details" });
    await details.getByRole("button", { name: "Execute", exact: true }).click();
    await details.getByRole("link", { name: "Result", exact: true }).click();
    const results = page.getByRole("region", { name: "Query results" });
    await expect(results.getByText("1001", { exact: true })).toBeVisible();
    const exportButton = results.getByRole("button", { name: "Export CSV", exact: true });
    // The export covers the whole snapshot, so a view change while it streams must neither drop it nor leave its button disabled.
    const viewChanges: Array<{ name: string; change: () => Promise<void> }> = [
      { name: "page", change: () => results.getByRole("button", { name: "Next page", exact: true }).click() },
      { name: "sort", change: async () => { await results.getByLabel("Sort by").selectOption({ index: 1 }); } },
      { name: "filter", change: async () => { await results.getByLabel("Filter results").fill("10"); await results.getByRole("button", { name: "Filter results", exact: true }).click(); } },
    ];
    for (const viewChange of viewChanges) {
      let release = () => {};
      const held = new Promise<void>((resolve) => { release = resolve; });
      await page.route("**/portcullis.v1.QueryExecutions/ExportCSV", async (route) => { await held; await route.continue(); });
      await exportButton.click();
      await expect(exportButton, `export disabled while streaming (${viewChange.name})`).toBeDisabled();
      await viewChange.change();
      release();
      await expect(results.getByRole("link", { name: "Download CSV", exact: true }), `export finished after ${viewChange.name} change`).toBeVisible();
      await expect(exportButton, `export enabled after ${viewChange.name} change`).toBeEnabled();
      await page.unroute("**/portcullis.v1.QueryExecutions/ExportCSV");
    }

    // Failure path: an export refused after the view changed still reports its error and re-enables the button.
    let releaseFailure = () => {};
    const heldFailure = new Promise<void>((resolve) => { releaseFailure = resolve; });
    await page.route("**/portcullis.v1.QueryExecutions/ExportCSV", async (route) => { await heldFailure; await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ code: "unavailable", message: "simulated failure" }) }); });
    await exportButton.click();
    await results.getByLabel("Sort by").selectOption({ index: 0 });
    releaseFailure();
    await expect(results.getByText(/^CSV export failed:/)).toBeVisible();
    await expect(exportButton).toBeEnabled();
    await page.unroute("**/portcullis.v1.QueryExecutions/ExportCSV");
  });

  test("result errors name expiry only for a missing snapshot and keep export failures separate", async ({ page }) => {
    await page.goto("/requests");
    await page.getByRole("row", { name: /Result paging check/ }).getByRole("link", { name: "Result" }).click();
    const results = page.getByRole("region", { name: "Query results" });
    await expect(results.getByText("1001", { exact: true })).toBeVisible();

    const restoreExport = await failProcedure(page, "QueryExecutions/ExportCSV", "unavailable");
    await results.getByRole("button", { name: "Export CSV", exact: true }).click();
    // A streaming call may surface the HTTP status rather than the JSON message, so only the separate export prefix is asserted.
    await expect(results.getByText(/^CSV export failed:/)).toBeVisible();
    await expect(results.getByText(/expired or been evicted/)).toHaveCount(0);
    await expect(results.getByText("1001", { exact: true })).toBeVisible();
    await restoreExport();

    const restoreUnavailable = await failProcedure(page, "QueryExecutions/GetResult", "unavailable");
    await results.getByRole("button", { name: "Next page", exact: true }).click();
    await expect(results.getByRole("alert").filter({ hasText: "simulated failure" }).first()).toBeVisible();
    await expect(results.getByText(/expired or been evicted/)).toHaveCount(0);
    await restoreUnavailable();

    await page.route("**/portcullis.v1.QueryExecutions/GetResult", (route) => route.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({ code: "failed_precondition", message: "result unavailable or expired" }),
    }));
    await page.reload();
    await expect(page.getByText("result unavailable or expired Results may have expired or been evicted.")).toBeVisible();
    await page.unroute("**/portcullis.v1.QueryExecutions/GetResult");
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
