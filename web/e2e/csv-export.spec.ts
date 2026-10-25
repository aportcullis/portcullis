import { create, fromBinary, toBinary } from "@bufbuild/protobuf";
import { expect, test, type Download, type Locator, type Page, type Route } from "@playwright/test";
import assert from "node:assert/strict";

import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import {
  ExportQueryCSVRequestSchema,
  LogicalType,
  QueryCSVChunkSchema,
  QueryExecutionSchema,
  QueryResultPageSchema,
} from "@/gen/portcullis/v1/query_executions_pb";
import { signInForScenario } from "@e2e/login";
import { fulfillConnectError } from "@e2e/rpc";

const requestID = "00000000-0000-4000-8000-000000000601";
const safeCSV = "entry_id,note\r\n9007199254740993,'=formula\r\n9007199254740994,ordinary\r\n";
const rawCSV = "entry_id,note\r\n9007199254740993,=formula\r\n9007199254740994,ordinary\r\n";

/** Holds or refuses an export while keeping real login and session checks. */
type ExportOverride = (route: Route, raw: boolean) => Promise<boolean>;

/** Selects the fixture's normal server reply or a deliberately controlled export. */
type ExportFixtureMode = { kind: "default" } | { kind: "override"; handle: ExportOverride };

/** Selects ordinary safe/raw content or an explicitly supplied size-limit fixture. */
type CSVFixtureContent = { kind: "standard" } | { kind: "custom"; csv: string };

/** Holds an intercepted stream only after the browser has actually requested it. */
type CapturedCSVExport = { kind: "captured"; route: Route };
type PendingCSVExport = { kind: "waiting" } | CapturedCSVExport;
type PendingCSVExportHandle = { current: PendingCSVExport };

/** Exposes only the fixture controls and observable result/export UI. */
type CSVWorkspace = {
  results: Locator;
  dialog: Locator;
  trigger: Locator;
  exportCalls: () => number;
  executionCalls: () => number;
  rawChoices: boolean[];
  setExportOverride: (override: ExportOverride) => void;
  resetExportOverride: () => void;
  setCSV: (csv: string) => void;
};

/** Narrows a pending handle only when an actual intercepted stream is available. */
function isCapturedExport(pending: PendingCSVExport): pending is CapturedCSVExport {
  return pending.kind === "captured";
}

/** Requires the expected intercepted stream before allowing a race fixture to settle it. */
function requirePendingCSV(handle: PendingCSVExportHandle): Route {
  const { current } = handle;
  assert(isCapturedExport(current), "Expected the browser to request a pending CSV export");
  const { route } = current;
  return route;
}

/** Encodes CSV bytes as a binary Connect stream with an explicit successful end. */
function csvStreamBody(csv: string): Buffer {
  const chunk = toBinary(QueryCSVChunkSchema, create(QueryCSVChunkSchema, { data: new TextEncoder().encode(csv) }));
  const messageHeader = Buffer.alloc(5);
  messageHeader.writeUInt32BE(chunk.byteLength, 1);
  const completion = Buffer.from("{}");
  const completionHeader = Buffer.alloc(5);
  completionHeader[0] = 2;
  completionHeader.writeUInt32BE(completion.byteLength, 1);
  return Buffer.concat([messageHeader, Buffer.from(chunk), completionHeader, completion]);
}

/** Finishes an intercepted export without replacing the production streaming decoder. */
async function fulfillCSV(route: Route, csv: string): Promise<void> {
  await route.fulfill({ status: 200, contentType: "application/connect+proto", body: csvStreamBody(csv) });
}

/** Reads the actual saved download instead of substituting a Blob fetch. */
async function savedCSV(download: Download): Promise<string> {
  expect(download.suggestedFilename()).toBe("query-result.csv");
  expect(await download.failure()).toBeNull();
  const stream = await download.createReadStream();
  assert(stream !== null, "Expected a saved CSV file stream");
  let contents = "";
  for await (const chunk of stream) contents += String(chunk);
  return contents;
}

/** Opens a result through the real binary's authentication with controlled binary result responses. */
async function openCSVWorkspace(page: Page): Promise<CSVWorkspace> {
  await page.goto("/login");
  await signInForScenario(page, "admin@example.com", "correct-horse-battery");
  const expiresAt = BigInt(Math.floor(Date.now() / 1000) + 900);
  let exportCalls = 0;
  let executionCalls = 0;
  let exportMode: ExportFixtureMode = { kind: "default" };
  let csvContent: CSVFixtureContent = { kind: "standard" };
  const rawChoices: boolean[] = [];
  await page.route("**/portcullis.v1.QueryExecutions/Execute", route => {
    executionCalls++;
    return fulfillConnectError(route, "internal", "CSV must not execute SQL");
  });
  await page.route("**/portcullis.v1.QueryExecutions/Get", route => route.fulfill({
    status: 200,
    contentType: "application/proto",
    body: Buffer.from(toBinary(QueryExecutionSchema, create(QueryExecutionSchema, {
      state: AccessRequestState.SUCCEEDED,
      resultAvailable: true,
      rowCount: 2n,
      resultExpiresAt: { seconds: expiresAt },
    }))),
  }));
  await page.route("**/portcullis.v1.QueryExecutions/GetResult", route => route.fulfill({
    status: 200,
    contentType: "application/proto",
    body: Buffer.from(toBinary(QueryResultPageSchema, create(QueryResultPageSchema, {
      columns: [
        { name: "entry_id", logicalType: LogicalType.INT, dbTypeName: "int8" },
        { name: "note", logicalType: LogicalType.STRING, dbTypeName: "text" },
      ],
      rows: [
        { cells: [{ kind: { case: "intValue", value: "9007199254740993" } }, { kind: { case: "stringValue", value: "=formula" } }] },
        { cells: [{ kind: { case: "intValue", value: "9007199254740994" } }, { kind: { case: "stringValue", value: "ordinary" } }] },
      ],
      page: 1,
      pageSize: 50,
      totalCount: 2n,
      totalPages: 1,
      expiresAt: { seconds: expiresAt },
    }))),
  }));
  await page.route("**/portcullis.v1.QueryExecutions/ExportCSV", async route => {
    const payload = route.request().postDataBuffer();
    assert(Buffer.isBuffer(payload), "Expected a binary export request");
    assert(payload.length >= 5 && payload[0] === 0 && payload.readUInt32BE(1) === payload.length - 5,
      "Expected one uncompressed Connect request envelope");
    const { raw } = fromBinary(ExportQueryCSVRequestSchema, payload.subarray(5));
    exportCalls++;
    rawChoices.push(raw);
    if (exportMode.kind === "override" && await exportMode.handle(route, raw)) return;
    const csv = csvContent.kind === "custom" ? csvContent.csv : raw ? rawCSV : safeCSV;
    await fulfillCSV(route, csv);
  });
  await page.goto(`/requests/${requestID}/result`);
  const results = page.getByRole("region", { name: "Query results", exact: true });
  await expect(results.getByText("9007199254740993", { exact: true })).toBeVisible();
  return {
    results,
    dialog: page.getByRole("dialog", { name: "Export CSV", exact: true }),
    trigger: results.getByRole("button", { name: "Export CSV", exact: true }),
    exportCalls: () => exportCalls,
    executionCalls: () => executionCalls,
    rawChoices,
    setExportOverride: handle => { exportMode = { kind: "override", handle }; },
    resetExportOverride: () => { exportMode = { kind: "default" }; },
    setCSV: csv => { csvContent = { kind: "custom", csv }; },
  };
}

/** Requires one native file action in the footer once preparation has finished. */
async function expectSingleFileAction(dialog: Locator): Promise<void> {
  const link = dialog.getByRole("link", { name: "Save CSV file", exact: true });
  await expect(link).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Download CSV", exact: true })).toHaveCount(0);
  expect(await link.evaluate(element => {
    const { parentElement } = element;
    if (!(parentElement instanceof HTMLElement)) throw new Error("Expected the native save action's footer");
    const buttons = Array.from(parentElement.querySelectorAll("button"));
    return parentElement.lastElementChild === element &&
      buttons.some(button => button.textContent === "Close export") &&
      buttons.some(button => button.textContent === "Copy CSV");
  })).toBe(true);
}

/** Prepares only when necessary, then verifies the native file action's actual saved bytes. */
async function downloadCSV(page: Page, workspace: CSVWorkspace): Promise<string> {
  const link = workspace.dialog.getByRole("link", { name: "Save CSV file", exact: true });
  if (!(await link.isVisible())) {
    await workspace.dialog.getByRole("button", { name: "Download CSV", exact: true }).click();
  }
  await expectSingleFileAction(workspace.dialog);
  const pending = page.waitForEvent("download");
  await link.click();
  return savedCSV(await pending);
}

test("open and configure CSV export without a request, then cancel and restore focus", async ({ page }) => {
  const workspace = await openCSVWorkspace(page);
  await workspace.trigger.click();
  await expect(workspace.dialog).toBeVisible();
  await expect(workspace.dialog.getByText("Whole cached snapshot in original query order, regardless of the visible filter or sort.", { exact: true })).toBeVisible();
  await expect(workspace.dialog.getByRole("checkbox", { name: "Spreadsheet-safe output", exact: true })).toBeChecked();
  await workspace.dialog.getByRole("checkbox", { name: "Spreadsheet-safe output", exact: true }).uncheck();
  await expect(workspace.dialog.getByText("Raw CSV can run formulas when opened in spreadsheet software.", { exact: true })).toBeVisible();
  await expect(workspace.dialog.getByRole("button", { name: "Copy CSV", exact: true })).toBeDisabled();
  await expect(workspace.dialog.getByRole("button", { name: "Download CSV", exact: true })).toBeDisabled();
  await workspace.dialog.getByRole("button", { name: "Close export", exact: true }).click();
  await expect(workspace.dialog).toHaveCount(0);
  await expect(workspace.trigger).toBeFocused();
  expect(workspace.exportCalls()).toBe(0);
  expect(workspace.executionCalls()).toBe(0);
});

test("safe CSV clipboard and native file destinations contain equivalent complete output", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const workspace = await openCSVWorkspace(page);
  await workspace.trigger.click();
  await workspace.dialog.getByRole("button", { name: "Copy CSV", exact: true }).click();
  await expect(workspace.dialog.getByText("Copied CSV to clipboard.", { exact: true })).toBeVisible();
  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(copied).toBe(safeCSV);
  expect(await downloadCSV(page, workspace)).toBe(copied);
  await workspace.dialog.getByRole("button", { name: "Close export", exact: true }).click();
  await expect(workspace.results.getByRole("link", { name: "Save CSV file", exact: true })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Save CSV file", exact: true })).toHaveCount(0);
  expect(workspace.rawChoices.every(raw => !raw)).toBe(true);
  expect(workspace.executionCalls()).toBe(0);
});

test("raw CSV requires explicit risk acknowledgement and reopening restores safe output", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const workspace = await openCSVWorkspace(page);
  await workspace.trigger.click();
  await workspace.dialog.getByRole("checkbox", { name: "Spreadsheet-safe output", exact: true }).uncheck();
  await expect(workspace.dialog.getByRole("button", { name: "Copy CSV", exact: true })).toBeDisabled();
  await workspace.dialog.getByRole("checkbox", { name: "I understand the raw CSV risk", exact: true }).check();
  await workspace.dialog.getByRole("button", { name: "Copy CSV", exact: true }).click();
  await expect(workspace.dialog.getByText("Copied CSV to clipboard.", { exact: true })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(rawCSV);
  expect(await downloadCSV(page, workspace)).toBe(rawCSV);
  expect(workspace.rawChoices.every(raw => raw)).toBe(true);
  await workspace.dialog.getByRole("button", { name: "Close export", exact: true }).click();
  await workspace.trigger.click();
  await expect(workspace.dialog.getByRole("checkbox", { name: "Spreadsheet-safe output", exact: true })).toBeChecked();
  await expect(workspace.dialog.getByRole("checkbox", { name: "I understand the raw CSV risk", exact: true })).toHaveCount(0);
  expect(workspace.executionCalls()).toBe(0);
});

test("preparing a native CSV file transfers focus to the only footer file action", async ({ page }) => {
  const workspace = await openCSVWorkspace(page);
  await workspace.trigger.click();
  await expect(workspace.dialog.getByRole("checkbox", { name: "Spreadsheet-safe output", exact: true })).toBeFocused();
  const prepareButton = workspace.dialog.getByRole("button", { name: "Download CSV", exact: true });
  await expect(prepareButton).toBeVisible();
  await prepareButton.focus();
  await expect(prepareButton).toBeFocused();
  await prepareButton.press("Enter");
  const link = workspace.dialog.getByRole("link", { name: "Save CSV file", exact: true });
  await expect(link).toBeVisible();
  await expect(link).toBeFocused();
  await expectSingleFileAction(workspace.dialog);
  expect(await downloadCSV(page, workspace)).toBe(safeCSV);
  expect(workspace.executionCalls()).toBe(0);
});

test("finishing a held CSV preparation preserves focus the user moved to Close export", async ({ page }) => {
  const workspace = await openCSVWorkspace(page);
  const pending: PendingCSVExportHandle = { current: { kind: "waiting" } };
  workspace.setExportOverride(async route => { pending.current = { kind: "captured", route }; return true; });
  await workspace.trigger.click();
  await expect(workspace.dialog.getByRole("checkbox", { name: "Spreadsheet-safe output", exact: true })).toBeFocused();
  const prepareButton = workspace.dialog.getByRole("button", { name: "Download CSV", exact: true });
  await expect(prepareButton).toBeVisible();
  await prepareButton.focus();
  await expect(prepareButton).toBeFocused();
  await prepareButton.press("Enter");
  await expect.poll(() => pending.current.kind).toBe("captured");
  const closeButton = workspace.dialog.getByRole("button", { name: "Close export", exact: true });
  await closeButton.focus();
  await expect(closeButton).toBeFocused();
  await fulfillCSV(requirePendingCSV(pending), safeCSV);
  await expect(workspace.dialog.getByRole("link", { name: "Save CSV file", exact: true })).toBeVisible();
  await expect(closeButton).toBeFocused();
  await expectSingleFileAction(workspace.dialog);
  expect(workspace.executionCalls()).toBe(0);
});

test("the export dialog traps keyboard focus, returns it on Escape and fits 320 pixels", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 900 });
  const workspace = await openCSVWorkspace(page);
  await workspace.trigger.focus();
  await page.keyboard.press("Enter");
  await expect(workspace.dialog).toHaveAttribute("aria-modal", "true");
  await expect(workspace.dialog.getByRole("checkbox", { name: "Spreadsheet-safe output", exact: true })).toBeFocused();
  for (let index = 0; index < 12; index++) {
    await page.keyboard.press("Tab");
    expect(await workspace.dialog.evaluate(element => {
      const { activeElement } = document;
      return activeElement instanceof HTMLElement && element.contains(activeElement);
    })).toBe(true);
  }
  for (let index = 0; index < 6; index++) {
    await page.keyboard.press("Shift+Tab");
    expect(await workspace.dialog.evaluate(element => {
      const { activeElement } = document;
      return activeElement instanceof HTMLElement && element.contains(activeElement);
    })).toBe(true);
  }
  const bounds = await workspace.dialog.boundingBox();
  assert(bounds !== null, "Expected an export dialog layout box");
  const { x: dialogLeft, width: dialogWidth } = bounds;
  expect(dialogLeft).toBeGreaterThanOrEqual(0);
  expect(dialogLeft + dialogWidth).toBeLessThanOrEqual(320);
  await page.keyboard.press("Escape");
  await expect(workspace.dialog).toHaveCount(0);
  await expect(workspace.trigger).toBeFocused();
  expect(workspace.exportCalls()).toBe(0);
});

test("CSV above the clipboard byte limit remains downloadable without attempting clipboard output", async ({ page }) => {
  const workspace = await openCSVWorkspace(page);
  const oversizedCSV = `${safeCSV}${"x".repeat(2 * 1024 * 1024)}`;
  workspace.setCSV(oversizedCSV);
  await page.evaluate(() => Object.defineProperty(navigator.clipboard, "writeText", {
    configurable: true,
    value: async () => { throw new Error("Oversized CSV must not reach clipboard output"); },
  }));
  await workspace.trigger.click();
  await workspace.dialog.getByRole("button", { name: "Copy CSV", exact: true }).click();
  await expect(workspace.dialog.getByRole("alert")).toHaveText("CSV exceeds the 2 MiB clipboard limit. Download the file instead.");
  await expect(workspace.dialog).toBeVisible();
  const downloaded = await downloadCSV(page, workspace);
  expect(downloaded.length).toBe(oversizedCSV.length);
  expect(downloaded.startsWith(safeCSV)).toBe(true);
  expect(downloaded.endsWith("x".repeat(100))).toBe(true);
  expect(workspace.executionCalls()).toBe(0);
});

test("clipboard denial keeps the dialog open and preserves native download fallback", async ({ page }) => {
  const workspace = await openCSVWorkspace(page);
  await page.evaluate(() => Object.defineProperty(navigator.clipboard, "writeText", {
    configurable: true,
    value: async () => { throw new DOMException("Denied", "NotAllowedError"); },
  }));
  await workspace.trigger.click();
  await workspace.dialog.getByRole("button", { name: "Copy CSV", exact: true }).click();
  await expect(workspace.dialog.getByRole("alert")).toHaveText("Clipboard access was denied or unavailable. Download the file instead.");
  await expect(workspace.dialog).toBeVisible();
  await expectSingleFileAction(workspace.dialog);
  expect(await downloadCSV(page, workspace)).toBe(safeCSV);
  expect(workspace.executionCalls()).toBe(0);
});

test("cancelled preparation and a closed dialog fence obsolete completion after reopening", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const workspace = await openCSVWorkspace(page);
  await page.evaluate(() => navigator.clipboard.writeText("clipboard before cancelled export"));
  const pending: PendingCSVExportHandle = { current: { kind: "waiting" } };
  workspace.setExportOverride(async route => { pending.current = { kind: "captured", route }; return true; });
  await workspace.trigger.click();
  await workspace.dialog.getByRole("button", { name: "Copy CSV", exact: true }).click();
  await expect.poll(() => pending.current.kind).toBe("captured");
  await expect(workspace.dialog.getByRole("status")).toContainText(/Preparing CSV: \d+ bytes/);
  await workspace.dialog.getByRole("button", { name: "Cancel export", exact: true }).click();
  await workspace.dialog.getByRole("button", { name: "Close export", exact: true }).click();
  await workspace.trigger.click();
  await fulfillCSV(requirePendingCSV(pending), "obsolete,csv\r\n");
  await expect(workspace.dialog.getByText("Copied CSV to clipboard.", { exact: true })).toHaveCount(0);
  await expect(workspace.dialog.getByRole("link", { name: "Save CSV file", exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("clipboard before cancelled export");
  workspace.resetExportOverride();
  await workspace.dialog.getByRole("button", { name: "Copy CSV", exact: true }).click();
  await expect(workspace.dialog.getByText("Copied CSV to clipboard.", { exact: true })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(safeCSV);
  expect(workspace.executionCalls()).toBe(0);
});

test("a refused export reports its failure inside the dialog without an expiry claim or write replay", async ({ page }) => {
  const workspace = await openCSVWorkspace(page);
  workspace.setExportOverride(async route => { await fulfillConnectError(route, "unavailable", "Export temporarily unavailable"); return true; });
  await workspace.trigger.click();
  await workspace.dialog.getByRole("button", { name: "Download CSV", exact: true }).click();
  await expect(workspace.dialog.getByRole("alert")).toBeVisible();
  await expect(workspace.dialog.getByText(/expired|evicted/)).toHaveCount(0);
  await expect(workspace.dialog.getByRole("button", { name: "Download CSV", exact: true })).toBeEnabled();
  await expect(workspace.dialog.getByRole("link", { name: "Save CSV file", exact: true })).toHaveCount(0);
  expect(workspace.exportCalls()).toBe(1);
  expect(workspace.executionCalls()).toBe(0);
});

test("revoked result permission closes export and removes previously cached output", async ({ page }) => {
  const workspace = await openCSVWorkspace(page);
  workspace.setExportOverride(async route => { await fulfillConnectError(route, "permission_denied", "Result access revoked"); return true; });
  await workspace.trigger.click();
  await workspace.dialog.getByRole("button", { name: "Download CSV", exact: true }).click();
  await expect(workspace.dialog).toHaveCount(0);
  await expect(workspace.results.getByRole("alert")).toHaveText("Access to this result is no longer available.");
  await expect(workspace.results.getByText("9007199254740993", { exact: true })).toHaveCount(0);
  await expect(workspace.results.getByRole("button", { name: "Copy visible rows", exact: true })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Save CSV file", exact: true })).toHaveCount(0);
  expect(workspace.exportCalls()).toBe(1);
  expect(workspace.executionCalls()).toBe(0);
});

for (const loss of ["authority", "route", "TTL"]) {
  test(`${loss} loss disposes the dialog and fences a pending clipboard export`, async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    if (loss === "TTL") await page.clock.install();
    const workspace = await openCSVWorkspace(page);
    await page.evaluate(() => navigator.clipboard.writeText("clipboard before lost result"));
    const pending: PendingCSVExportHandle = { current: { kind: "waiting" } };
    workspace.setExportOverride(async route => { pending.current = { kind: "captured", route }; return true; });
    await workspace.trigger.click();
    await workspace.dialog.getByRole("button", { name: "Copy CSV", exact: true }).click();
    await expect.poll(() => pending.current.kind).toBe("captured");
    if (loss === "authority") {
      await page.route("**/portcullis.v1.Auth/Me", route => fulfillConnectError(route, "unavailable"));
      await page.evaluate(() => window.dispatchEvent(new Event("online")));
      await expect(page.getByRole("heading", { name: "Portcullis is temporarily unreachable" })).toBeVisible();
    } else if (loss === "route") {
      await page.goto("/connections");
      await expect(page.getByRole("heading", { name: "Connections", exact: true })).toBeVisible();
    } else {
      await page.clock.fastForward(901_000);
      await expect(workspace.results.getByRole("alert")).toContainText(/expired|evicted/);
    }
    await fulfillCSV(requirePendingCSV(pending), "obsolete,csv\r\n");
    await expect(workspace.dialog).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Save CSV file", exact: true })).toHaveCount(0);
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("clipboard before lost result");
    expect(workspace.executionCalls()).toBe(0);
  });
}
