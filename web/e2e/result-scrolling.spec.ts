import { create, fromBinary, toBinary } from "@bufbuild/protobuf";
import { expect, test, type Locator, type Page, type Route } from "@playwright/test";

import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import {
  GetQueryResultRequestSchema,
  LogicalType,
  QueryExecutionSchema,
  QueryResultPageSchema,
  QueryResultRowSchema,
  type GetQueryResultRequest,
} from "@/gen/portcullis/v1/query_executions_pb";
import { signInForScenario } from "@e2e/login";
import { fulfillConnectError } from "@e2e/rpc";

const requestID = "00000000-0000-4000-8000-000000000600";
const population = 600;
const rowHeight = 40;
const resultReadPattern = "**/portcullis.v1.QueryExecutions/GetResult";

/** Optionally owns a result reply so scenarios can expose failed or superseded reads. */
type ResultReadOverride = (route: Route, request: GetQueryResultRequest) => Promise<boolean>;

/** Retains an intercepted read without replacing the application's actual authentication. */
type PendingResultRead = {
  route: Route;
  request: GetQueryResultRequest;
  expiresAt: bigint;
};

/** Exposes the fixture's observable reads and mounted result controls. */
type ResultWorkspace = {
  panel: Locator;
  region: Locator;
  reads: GetQueryResultRequest[];
  expiresAt: bigint;
  executionCalls: () => number;
  setReadOverride: (override: ResultReadOverride | undefined) => void;
};

/** Builds stable row identities whose membership and ordering distinguish competing views. */
function rowLabel(number: number): string {
  return `row-${String(number).padStart(4, "0")}`;
}

/** Returns the real binary wire format for one bounded page of the synthetic snapshot. */
function resultBody(request: GetQueryResultRequest, expiresAt: bigint, marker = ""): Buffer {
  let numbers = Array.from({ length: population }, (_, index) => index + 1);
  if (request.filter !== "") {
    numbers = numbers.filter(number =>
      `${rowLabel(number)}\tgroup-${number % 2 === 0 ? "even" : "odd"}`.includes(request.filter));
  }
  if (request.sortColumn !== undefined && request.descending) numbers.reverse();
  const pageSize = request.pageSize;
  const selected = numbers.slice((request.page - 1) * pageSize, request.page * pageSize);
  const snapshot = create(QueryResultPageSchema, {
    columns: [
      { name: "entry_id", logicalType: LogicalType.STRING, dbTypeName: "text" },
      { name: "cohort", logicalType: LogicalType.STRING, dbTypeName: "text" },
    ],
    rows: selected.map(number => create(QueryResultRowSchema, {
      cells: [
        { kind: { case: "stringValue", value: rowLabel(number) } },
        { kind: { case: "stringValue", value: marker || `group-${number % 2 === 0 ? "even" : "odd"}` } },
      ],
    })),
    page: request.page,
    pageSize,
    totalCount: BigInt(numbers.length),
    totalPages: Math.ceil(numbers.length / pageSize),
    expiresAt: { seconds: expiresAt },
  });
  return Buffer.from(toBinary(QueryResultPageSchema, snapshot));
}

/** Completes a held read, permitting its caller to assert that stale data stays fenced. */
async function releaseResult(pending: PendingResultRead | undefined, marker = ""): Promise<void> {
  if (pending === undefined) throw new Error("Expected an intercepted result read");
  await pending.route.fulfill({
    status: 200,
    contentType: "application/proto",
    body: resultBody(pending.request, pending.expiresAt, marker),
  });
}

/** Logs in against the binary and substitutes only execution and result responses. */
async function openResultWorkspace(page: Page, initialOverride?: ResultReadOverride): Promise<ResultWorkspace> {
  await page.goto("/login");
  await signInForScenario(page, "admin@example.com", "correct-horse-battery");
  const reads: GetQueryResultRequest[] = [];
  const expiresAt = BigInt(Math.floor(Date.now() / 1000) + 900);
  let executionCalls = 0;
  let override = initialOverride;
  await page.route("**/portcullis.v1.QueryExecutions/Execute", route => {
    executionCalls++;
    return fulfillConnectError(route, "internal", "A result read must not execute SQL");
  });
  await page.route("**/portcullis.v1.QueryExecutions/Get", route => route.fulfill({
    status: 200,
    contentType: "application/proto",
    body: Buffer.from(toBinary(QueryExecutionSchema, create(QueryExecutionSchema, {
      state: AccessRequestState.SUCCEEDED,
      resultAvailable: true,
      rowCount: BigInt(population),
      resultExpiresAt: { seconds: expiresAt },
    }))),
  }));
  await page.route(resultReadPattern, async route => {
    const payload = route.request().postDataBuffer();
    if (payload === null) throw new Error("Expected a binary GetResult request");
    const request = fromBinary(GetQueryResultRequestSchema, payload);
    reads.push(request);
    if (override !== undefined && await override(route, request)) return;
    await route.fulfill({ status: 200, contentType: "application/proto", body: resultBody(request, expiresAt) });
  });
  await page.goto(`/requests/${requestID}/result`);
  const panel = page.getByRole("region", { name: "Query results", exact: true });
  const region = panel.getByRole("region", { name: "Scrollable result rows", exact: true });
  await expect(region).toBeVisible();
  await expect(panel.getByText(/Rows 1–10 of 600/)).toBeVisible();
  return {
    panel,
    region,
    reads,
    expiresAt,
    executionCalls: () => executionCalls,
    setReadOverride: next => { override = next; },
  };
}

/** Moves the actual viewport to an exact row boundary rather than synthesizing another query. */
async function scrollToRow(workspace: ResultWorkspace, index: number): Promise<void> {
  await workspace.region.evaluate((element, offset) => {
    element.scrollTop = offset;
    element.dispatchEvent(new Event("scroll"));
  }, index * rowHeight);
  await expect(workspace.panel.getByText(new RegExp(`Rows ${index + 1}–${index + 10} of 600`))).toBeVisible();
  await expect(workspace.region.getByText(rowLabel(index + 1), { exact: true })).toBeVisible();
}

/** Checks browser retention, rendering and read admission independently of returned content. */
async function expectBoundedWindow(workspace: ResultWorkspace): Promise<void> {
  for (const attribute of ["data-retained-pages", "data-retained-bytes", "data-in-flight"]) {
    await expect(workspace.region).toHaveAttribute(attribute, /^\d+$/);
  }
  await expect.poll(() => workspace.region.getByRole("row").filter({ hasText: /row-\d{4}/ }).count()).toBeLessThanOrEqual(19);
  expect(Number(await workspace.region.getAttribute("data-retained-pages"))).toBeLessThanOrEqual(5);
  expect(Number(await workspace.region.getAttribute("data-retained-bytes"))).toBeLessThanOrEqual(64 * 1024 * 1024);
  expect(Number(await workspace.region.getAttribute("data-in-flight"))).toBeLessThanOrEqual(2);
  expect(workspace.reads.every(request => request.pageSize === 50)).toBe(true);
  expect(workspace.executionCalls()).toBe(0);
}

test("600 snapshot rows scroll forward and backward with eviction and bounded refetch without SQL replay", async ({ page }) => {
  const workspace = await openResultWorkspace(page);
  await expectBoundedWindow(workspace);
  for (const index of [80, 180, 280, 380, 480, 550]) {
    await scrollToRow(workspace, index);
    await expectBoundedWindow(workspace);
  }
  const initialPageReads = workspace.reads.filter(request => request.page === 1).length;
  await scrollToRow(workspace, 0);
  await expect.poll(() => workspace.reads.filter(request => request.page === 1).length).toBeGreaterThan(initialPageReads);
  await expectBoundedWindow(workspace);
});

test("sorting and filtering reset a distant viewport and derive a new bounded result view", async ({ page }) => {
  const workspace = await openResultWorkspace(page);
  await scrollToRow(workspace, 350);
  await workspace.panel.getByRole("button", { name: /entry_id/ }).click();
  await expect(workspace.panel.getByText(/Rows 1–10 of 600/)).toBeVisible();
  await expect(workspace.region).toHaveJSProperty("scrollTop", 0);
  await workspace.panel.getByRole("button", { name: /entry_id/ }).click();
  await expect(workspace.region.getByText(rowLabel(600), { exact: true })).toBeVisible();
  await workspace.panel.getByLabel("Filter results", { exact: true }).fill("group-even");
  await workspace.panel.getByRole("button", { name: "Filter results", exact: true }).click();
  await expect(workspace.panel.getByText(/Rows 1–10 of 300/)).toBeVisible();
  await expect(workspace.region.getByText(rowLabel(599), { exact: true })).toHaveCount(0);
  await expectBoundedWindow(workspace);
});

test("Text and clipboard include the ten visible rows and exclude overscan and prefetched rows", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const workspace = await openResultWorkspace(page);
  await scrollToRow(workspace, 20);
  await workspace.panel.getByRole("button", { name: "Copy visible rows", exact: true }).click();
  await expect(workspace.panel.getByText("Copied 10 visible rows with column headers.", { exact: true })).toBeVisible();
  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(copied.split("\n").filter(line => line !== "")).toHaveLength(11);
  expect(copied).toContain(rowLabel(21));
  expect(copied).toContain(rowLabel(30));
  for (const number of [20, 31, 51]) expect(copied).not.toContain(rowLabel(number));
  await workspace.panel.getByRole("button", { name: "Text", exact: true }).click();
  const text = workspace.panel.getByLabel("Text results", { exact: true });
  await expect(text).toContainText(rowLabel(21));
  await expect(text).toContainText(rowLabel(30));
  for (const number of [20, 31, 51]) await expect(text).not.toContainText(rowLabel(number));
  expect(workspace.executionCalls()).toBe(0);
});

test("a half-row scroll includes both partially visible rows in Text and clipboard without overscan", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const workspace = await openResultWorkspace(page);
  await workspace.region.evaluate(element => {
    element.scrollTop = 20;
    element.dispatchEvent(new Event("scroll"));
  });
  await expect(workspace.panel.getByText("Rows 1–11 of 600", { exact: true })).toBeVisible();
  await workspace.panel.getByRole("button", { name: "Copy visible rows", exact: true }).click();
  await expect(workspace.panel.getByText("Copied 11 visible rows with column headers.", { exact: true })).toBeVisible();
  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(copied.split("\n").filter(line => line !== "")).toHaveLength(12);
  for (let number = 1; number <= 11; number++) expect(copied).toContain(rowLabel(number));
  expect(copied).not.toContain(rowLabel(12));
  await expectBoundedWindow(workspace);
  await workspace.panel.getByRole("button", { name: "Text", exact: true }).click();
  const text = workspace.panel.getByLabel("Text results", { exact: true });
  for (let number = 1; number <= 11; number++) await expect(text).toContainText(rowLabel(number));
  await expect(text).not.toContainText(rowLabel(12));
  expect(workspace.executionCalls()).toBe(0);
});

test("the result viewport supports keyboard navigation and preserves the 320-pixel page width", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 900 });
  const workspace = await openResultWorkspace(page);
  await workspace.region.focus();
  await expect(workspace.region).toBeFocused();
  await page.keyboard.press("PageDown");
  await expect.poll(() => workspace.region.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
  await expect(workspace.region).toBeFocused();
  await page.keyboard.press("End");
  await expect(workspace.region.getByText(rowLabel(600), { exact: true })).toBeVisible();
  await page.keyboard.press("Home");
  await expect(workspace.panel.getByText(/Rows 1–10 of 600/)).toBeVisible();
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
  await expectBoundedWindow(workspace);
});

test("neighbor-page prefetch and a nearby scroll preserve the focused result cell element", async ({ page }) => {
  let captured: { route: Route; request: GetQueryResultRequest } | undefined;
  const workspace = await openResultWorkspace(page, async (route, request) => {
    if (request.page !== 2) return false;
    captured = { route, request };
    return true;
  });
  await expect.poll(() => captured !== undefined).toBe(true);
  const firstCell = workspace.region.getByRole("button", { name: rowLabel(1), exact: true });
  await firstCell.focus();
  const focusedElement = await firstCell.elementHandle();
  if (focusedElement === null || captured === undefined) throw new Error("Expected a focused cell and pending neighbor page");
  await releaseResult({ ...captured, expiresAt: workspace.expiresAt });
  await expect(workspace.region).toHaveAttribute("data-in-flight", "0");
  await expect(firstCell).toBeFocused();
  expect(await focusedElement.evaluate(element => element === document.activeElement)).toBe(true);
  await scrollToRow(workspace, 1);
  await expect(firstCell).toBeFocused();
  expect(await focusedElement.evaluate(element => element === document.activeElement)).toBe(true);
  await expectBoundedWindow(workspace);
});

test("a late ascending result cannot replace a newer descending view", async ({ page }) => {
  const workspace = await openResultWorkspace(page);
  let pending: PendingResultRead | undefined;
  workspace.setReadOverride(async (route, request) => {
    if (request.sortColumn !== 0 || request.descending) return false;
    pending = { route, request, expiresAt: workspace.expiresAt };
    return true;
  });
  await workspace.panel.getByRole("button", { name: /entry_id/ }).click();
  await expect.poll(() => pending !== undefined).toBe(true);
  await workspace.panel.getByRole("button", { name: /entry_id/ }).click();
  await expect(workspace.region.getByText(rowLabel(600), { exact: true })).toBeVisible();
  await releaseResult(pending, "stale ascending response");
  await expect(workspace.panel.getByText("stale ascending response", { exact: true })).toHaveCount(0);
  await expect(workspace.region.getByText(rowLabel(600), { exact: true })).toBeVisible();
  await expectBoundedWindow(workspace);
});

test("an old ascending deadline cannot change a newer filtered view", async ({ page }) => {
  const workspace = await openResultWorkspace(page);
  const pendingRoutes: Route[] = [];
  workspace.setReadOverride(async (route, request) => {
    if (request.sortColumn !== 0 || request.descending || request.filter !== "") return false;
    pendingRoutes.push(route);
    return true;
  });
  await workspace.panel.getByRole("button", { name: /entry_id/ }).click();
  await expect.poll(() => pendingRoutes.length).toBe(1);
  const [pendingRoute] = pendingRoutes;
  if (!pendingRoute) throw new Error("Expected an intercepted ascending result read");
  await workspace.panel.getByLabel("Filter results", { exact: true }).fill("group-even");
  await workspace.panel.getByRole("button", { name: "Filter results", exact: true }).click();
  await expect(workspace.panel.getByText("Rows 1–10 of 300", { exact: true })).toBeVisible();
  await expect(workspace.region.getByText(rowLabel(2), { exact: true })).toBeVisible();
  await fulfillConnectError(pendingRoute, "deadline_exceeded", "request deadline exceeded");
  await expect(workspace.region).toHaveAttribute("data-in-flight", "0");
  await expect(workspace.panel.getByRole("alert")).toHaveCount(0);
  await expect(workspace.panel.getByText("Rows 1–10 of 300", { exact: true })).toBeVisible();
  await expect(workspace.region.getByText(rowLabel(2), { exact: true })).toBeVisible();
  await expectBoundedWindow(workspace);
});

test("a late filter result cannot repopulate a newer filter's cache or visible rows", async ({ page }) => {
  const workspace = await openResultWorkspace(page);
  let pending: PendingResultRead | undefined;
  workspace.setReadOverride(async (route, request) => {
    if (request.filter !== "group-even") return false;
    pending = { route, request, expiresAt: workspace.expiresAt };
    return true;
  });
  await workspace.panel.getByLabel("Filter results", { exact: true }).fill("group-even");
  await workspace.panel.getByRole("button", { name: "Filter results", exact: true }).click();
  await expect.poll(() => pending !== undefined).toBe(true);
  await workspace.panel.getByLabel("Filter results", { exact: true }).fill("group-odd");
  await workspace.panel.getByRole("button", { name: "Filter results", exact: true }).click();
  await expect(workspace.region.getByText(rowLabel(1), { exact: true })).toBeVisible();
  await expect(workspace.panel.getByText(/Rows 1–10 of 300/)).toBeVisible();
  await releaseResult(pending, "stale even response");
  await expect(workspace.panel.getByText("stale even response", { exact: true })).toHaveCount(0);
  await expect(workspace.region.getByText(rowLabel(2), { exact: true })).toHaveCount(0);
  await expectBoundedWindow(workspace);
});

test("a current filter deadline withholds rows and permits explicit Retry without SQL replay", async ({ page }) => {
  const workspace = await openResultWorkspace(page);
  let refuseFilter = true;
  workspace.setReadOverride(async (route, request) => {
    if (!refuseFilter || request.filter !== "group-even") return false;
    await fulfillConnectError(route, "deadline_exceeded", "request deadline exceeded");
    return true;
  });
  await workspace.panel.getByLabel("Filter results", { exact: true }).fill("group-even");
  await workspace.panel.getByRole("button", { name: "Filter results", exact: true }).click();
  await expect(workspace.panel.getByRole("alert")).toHaveText("request deadline exceeded");
  await expect(workspace.region.getByText(/^row-\d{4}$/)).toHaveCount(0);
  await expect(workspace.region).toHaveAttribute("data-retained-pages", "0");
  await expect(workspace.panel.getByText("Rows 0–0 of 0", { exact: true })).toBeVisible();
  await expect(workspace.panel.getByLabel("Filter results", { exact: true })).toHaveValue("group-even");
  const retryButton = workspace.panel.getByRole("button", { name: "Retry result", exact: true });
  await expect(retryButton).toBeVisible();
  expect(workspace.reads.filter(request => request.filter === "group-even")).toHaveLength(1);
  refuseFilter = false;
  await retryButton.click();
  await expect(workspace.panel.getByText("Rows 1–10 of 300", { exact: true })).toBeVisible();
  await expect(workspace.region.getByText(rowLabel(2), { exact: true })).toBeVisible();
  await expect(workspace.panel.getByRole("alert")).toHaveCount(0);
  await expectBoundedWindow(workspace);
});

test("a transient missing-page read permits explicit Retry without replaying execution", async ({ page }) => {
  const workspace = await openResultWorkspace(page);
  workspace.setReadOverride(async (route, request) => {
    if (request.page < 5) return false;
    await fulfillConnectError(route, "unavailable", "Snapshot page temporarily unavailable");
    return true;
  });
  await workspace.region.evaluate((element, offset) => {
    element.scrollTop = offset;
    element.dispatchEvent(new Event("scroll"));
  }, 250 * rowHeight);
  await expect(workspace.panel.getByRole("alert")).toContainText("Snapshot page temporarily unavailable");
  expect(workspace.executionCalls()).toBe(0);
  workspace.setReadOverride(undefined);
  await workspace.panel.getByRole("button", { name: "Retry result", exact: true }).click();
  await expect(workspace.region.getByText(rowLabel(251), { exact: true })).toBeVisible();
  await expect(workspace.panel.getByRole("alert")).toHaveCount(0);
  await expectBoundedWindow(workspace);
});

test("an expired snapshot clears already retained rows instead of leaving readable cached values", async ({ page }) => {
  const workspace = await openResultWorkspace(page);
  workspace.setReadOverride(async (route, request) => {
    if (request.page < 5) return false;
    await route.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({ code: "failed_precondition", message: "Snapshot expired" }),
    });
    return true;
  });
  await workspace.region.evaluate((element, offset) => {
    element.scrollTop = offset;
    element.dispatchEvent(new Event("scroll"));
  }, 250 * rowHeight);
  await expect(workspace.panel.getByRole("alert")).toContainText(/expired|evicted/);
  await expect(workspace.panel.getByText(/^row-\d{4}$/)).toHaveCount(0);
  await expect(workspace.panel.getByRole("button", { name: "Copy visible rows", exact: true })).toHaveCount(0);
  expect(workspace.executionCalls()).toBe(0);
});

test("local TTL expiry during held sort and filter reads clears cached output without SQL replay", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.clock.install();
  const workspace = await openResultWorkspace(page);
  await workspace.panel.getByRole("button", { name: "Copy visible rows", exact: true }).click();
  await expect(workspace.panel.getByText("Copied 10 visible rows with column headers.", { exact: true })).toBeVisible();
  const pending: PendingResultRead[] = [];
  workspace.setReadOverride(async (route, request) => {
    if (request.sortColumn !== 0) return false;
    pending.push({ route, request, expiresAt: workspace.expiresAt });
    return true;
  });
  await workspace.panel.getByRole("button", { name: /entry_id/ }).click();
  await expect.poll(() => pending.some(read => read.request.filter === "")).toBe(true);
  await workspace.panel.getByLabel("Filter results", { exact: true }).fill("group-even");
  await workspace.panel.getByRole("button", { name: "Filter results", exact: true }).click();
  await expect.poll(() => pending.some(read => read.request.filter === "group-even")).toBe(true);
  await page.clock.fastForward(901_000);
  await expect(workspace.panel.getByRole("alert")).toContainText(/expired|evicted/);
  await expect(workspace.panel.getByText(/^row-\d{4}$/)).toHaveCount(0);
  await expect(workspace.panel.getByRole("button", { name: "Copy visible rows", exact: true })).toHaveCount(0);
  await expect(workspace.panel.getByRole("button", { name: "Export CSV", exact: true })).toHaveCount(0);
  await expect(page.getByRole("dialog", { name: "Export CSV", exact: true })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Save CSV file", exact: true })).toHaveCount(0);
  await expect(workspace.panel.getByText(/Copied \d+ visible rows/)).toHaveCount(0);
  for (const read of pending) await releaseResult(read, "late after local expiry");
  await expect(workspace.panel.getByText("late after local expiry", { exact: true })).toHaveCount(0);
  await expect(workspace.panel.getByText(/^row-\d{4}$/)).toHaveCount(0);
  expect(workspace.executionCalls()).toBe(0);
});

test("authorization loss removes protected rows and fences a delayed page response", async ({ page }) => {
  const workspace = await openResultWorkspace(page);
  let pending: PendingResultRead | undefined;
  workspace.setReadOverride(async (route, request) => {
    if (request.page < 5) return false;
    pending = { route, request, expiresAt: workspace.expiresAt };
    return true;
  });
  await workspace.region.evaluate((element, offset) => {
    element.scrollTop = offset;
    element.dispatchEvent(new Event("scroll"));
  }, 250 * rowHeight);
  await expect.poll(() => pending !== undefined).toBe(true);
  await page.route("**/portcullis.v1.Auth/Me", route => fulfillConnectError(route, "unavailable"));
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(page.getByRole("heading", { name: "Portcullis is temporarily unreachable" })).toBeVisible();
  await releaseResult(pending, "late protected response");
  await expect(page.getByText("late protected response", { exact: true })).toHaveCount(0);
  await expect(page.getByText(/^row-\d{4}$/)).toHaveCount(0);
  expect(workspace.executionCalls()).toBe(0);
});

test("leaving the result route disposes retained pages and ignores an old route's delayed response", async ({ page }) => {
  const workspace = await openResultWorkspace(page);
  let pending: PendingResultRead | undefined;
  workspace.setReadOverride(async (route, request) => {
    if (request.page < 5) return false;
    pending = { route, request, expiresAt: workspace.expiresAt };
    return true;
  });
  await workspace.region.evaluate((element, offset) => {
    element.scrollTop = offset;
    element.dispatchEvent(new Event("scroll"));
  }, 250 * rowHeight);
  await expect.poll(() => pending !== undefined).toBe(true);
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: /Connections/ }).click();
  await expect(page).toHaveURL(/\/connections$/);
  await releaseResult(pending, "late disposed response");
  await expect(page.getByText("late disposed response", { exact: true })).toHaveCount(0);
  await expect(workspace.panel).toHaveCount(0);
  workspace.setReadOverride(undefined);
  const previousReads = workspace.reads.filter(request => request.page === 1).length;
  await page.goto(`/requests/${requestID}/result`);
  await expect(workspace.panel.getByText(/Rows 1–10 of 600/)).toBeVisible();
  await expect.poll(() => workspace.reads.filter(request => request.page === 1).length).toBeGreaterThan(previousReads);
  await expectBoundedWindow(workspace);
});
