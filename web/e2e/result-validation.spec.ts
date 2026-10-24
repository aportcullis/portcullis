import { create, fromBinary, toBinary } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { expect, test, type Locator, type Page, type Route } from "@playwright/test";
import assert from "node:assert/strict";

import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import {
  CellValueSchema,
  ColumnMetaSchema,
  GetQueryResultRequestSchema,
  LogicalType,
  QueryExecutionSchema,
  QueryResultPageSchema,
  QueryResultRowSchema,
  type CellValue,
  type ColumnMeta,
  type GetQueryResultRequest,
  type QueryResultPage,
} from "@/gen/portcullis/v1/query_executions_pb";
import { signInForScenario } from "@e2e/login";
import { fulfillConnectError } from "@e2e/rpc";

const requestID = "00000000-0000-4000-8000-000000000602";
const protectedText = "retained valid snapshot";
const invalidResponseMessage = "The result response is invalid.";

/** Selects valid wire values that must survive parsing without coercion. */
type ValidSample = "exact" | "literals" | "binary-floats" | "mixed-storage" | "empty" | "zero-columns" | "protected";
/** Names independently malformed replies, including forms protobuf can decode successfully. */
type InvalidSample = "unset-kind" | "false-null" | "unknown-enum" | "missing-expiry" |
  "invalid-expiry" | "invalid-page" | "wrong-row-width";
/** Deliberately supplies valid, invalid or delayed bytes to the actual application reader. */
type ResultReply = { kind: "valid"; sample: ValidSample } |
  { kind: "invalid"; sample: InvalidSample } |
  { kind: "held"; capture: (route: Route, request: GetQueryResultRequest) => void };
type CapturedResultRead = { kind: "captured"; route: Route };
type PendingResultRead = { current: { kind: "waiting" } | CapturedResultRead };
/** Exposes response choices and read/write observations without replacing authentication. */
type ResultWorkspace = {
  panel: Locator;
  reads: GetQueryResultRequest[];
  expiresAt: bigint;
  executionCalls: () => number;
  setReply: (reply: ResultReply) => void;
};

/** Creates one declared column while retaining the generated protocol contract. */
function column(name: string, logicalType: LogicalType): ColumnMeta {
  return create(ColumnMetaSchema, { name, logicalType, dbTypeName: "fixture", nullable: true });
}

/** Produces a coherent bounded page with explicitly chosen exact or special cell values. */
function validPage(sample: ValidSample, expiresAt: bigint): QueryResultPage {
  let columns: ColumnMeta[];
  let cells: CellValue[];
  switch (sample) {
    case "exact":
      columns = [column("signed_int", LogicalType.INT), column("unsigned_int", LogicalType.INT), column("exact_decimal", LogicalType.DECIMAL)];
      cells = [
        create(CellValueSchema, { kind: { case: "intValue", value: "9007199254740993" } }),
        create(CellValueSchema, { kind: { case: "intValue", value: "18446744073709551615" } }),
        create(CellValueSchema, { kind: { case: "decimalValue", value: "-19.9500000000000000001" } }),
      ];
      break;
    case "literals":
      columns = [column("empty", LogicalType.STRING), column("truth", LogicalType.BOOL), column("sql_null", LogicalType.STRING), column("literal_null", LogicalType.STRING)];
      cells = [
        create(CellValueSchema, { kind: { case: "stringValue", value: "" } }),
        create(CellValueSchema, { kind: { case: "boolValue", value: false } }),
        create(CellValueSchema, { kind: { case: "isNull", value: true } }),
        create(CellValueSchema, { kind: { case: "stringValue", value: "NULL" } }),
      ];
      break;
    case "binary-floats":
      columns = [column("binary", LogicalType.BYTES), column("not_number", LogicalType.FLOAT), column("positive", LogicalType.FLOAT), column("negative", LogicalType.FLOAT)];
      cells = [
        create(CellValueSchema, { kind: { case: "bytesValue", value: new Uint8Array([0, 255, 16]) } }),
        create(CellValueSchema, { kind: { case: "doubleValue", value: Number.NaN } }),
        create(CellValueSchema, { kind: { case: "doubleValue", value: Number.POSITIVE_INFINITY } }),
        create(CellValueSchema, { kind: { case: "doubleValue", value: Number.NEGATIVE_INFINITY } }),
      ];
      break;
    case "mixed-storage":
      columns = [column("declared_int", LogicalType.INT), column("opaque", LogicalType.UNKNOWN), column("observed_at", LogicalType.TIMESTAMPTZ)];
      cells = [
        create(CellValueSchema, { kind: { case: "stringValue", value: "storage-class-text" } }),
        create(CellValueSchema, { kind: { case: "stringValue", value: "opaque-custom-value" } }),
        create(CellValueSchema, { kind: { case: "temporalValue", value: "2026-10-24T00:00:00Z" } }),
      ];
      break;
    case "empty":
    case "protected":
      columns = [column("protected_value", LogicalType.STRING)];
      cells = [create(CellValueSchema, { kind: { case: "stringValue", value: protectedText } })];
      break;
    case "zero-columns":
      columns = [];
      cells = [];
      break;
  }
  const rows = sample === "empty" || sample === "zero-columns" ? [] : [create(QueryResultRowSchema, { cells })];
  return create(QueryResultPageSchema, {
    columns,
    rows,
    page: 1,
    pageSize: 50,
    totalCount: BigInt(rows.length),
    totalPages: 1,
    expiresAt: { seconds: expiresAt },
  });
}

/** Changes one response invariant without corrupting the binary transport itself. */
function invalidPage(sample: InvalidSample, expiresAt: bigint): QueryResultPage {
  const snapshot = validPage("protected", expiresAt);
  assert(snapshot.columns.length === 1 && snapshot.rows.length === 1, "Expected a single-cell invalid-response fixture");
  const [firstColumn] = snapshot.columns;
  const [firstRow] = snapshot.rows;
  switch (sample) {
    case "unset-kind":
      firstRow.cells = [create(CellValueSchema)];
      break;
    case "false-null":
      firstRow.cells = [create(CellValueSchema, { kind: { case: "isNull", value: false } })];
      break;
    case "unknown-enum":
      firstColumn.logicalType = Number("999");
      break;
    case "missing-expiry": {
      const { columns, rows, page, pageSize, totalCount, totalPages, truncated } = snapshot;
      return create(QueryResultPageSchema, { columns, rows, page, pageSize, totalCount, totalPages, truncated });
    }
    case "invalid-expiry":
      snapshot.expiresAt = create(TimestampSchema, { seconds: expiresAt, nanos: -1 });
      break;
    case "invalid-page":
      snapshot.page = 0;
      break;
    case "wrong-row-width":
      firstRow.cells = [];
      break;
  }
  return snapshot;
}

/** Narrows a race fixture only after its expected read was intercepted. */
function isCapturedRead(value: PendingResultRead["current"]): value is CapturedResultRead {
  return value.kind === "captured";
}

/** Requires an actual pending read instead of manufacturing an empty response target. */
function requirePendingRoute(pending: PendingResultRead): Route {
  const { current } = pending;
  assert(isCapturedRead(current), "Expected the browser to request a pending result read");
  const { route } = current;
  return route;
}

/** Answers through the generated protobuf decoder used by the actual SPA transport. */
async function fulfillPage(route: Route, snapshot: QueryResultPage): Promise<void> {
  await route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(QueryResultPageSchema, snapshot)) });
}

/** Uses real login and Me while selecting only controlled execution/result replies. */
async function openWorkspace(page: Page, initialReply: ResultReply): Promise<ResultWorkspace> {
  await page.goto("/login");
  await signInForScenario(page, "admin@example.com", "correct-horse-battery");
  const expiresAt = BigInt(Math.floor(Date.now() / 1000) + 900);
  const reads: GetQueryResultRequest[] = [];
  let reply = initialReply;
  let executionCalls = 0;
  await page.route("**/portcullis.v1.QueryExecutions/Execute", route => {
    executionCalls++;
    return fulfillConnectError(route, "internal", "Validating results must not execute SQL");
  });
  await page.route("**/portcullis.v1.QueryExecutions/Get", route => route.fulfill({
    status: 200,
    contentType: "application/proto",
    body: Buffer.from(toBinary(QueryExecutionSchema, create(QueryExecutionSchema, {
      state: AccessRequestState.SUCCEEDED,
      resultAvailable: true,
      rowCount: 1n,
      resultExpiresAt: { seconds: expiresAt },
    }))),
  }));
  await page.route("**/portcullis.v1.QueryExecutions/GetResult", async route => {
    const payload = route.request().postDataBuffer();
    assert(Buffer.isBuffer(payload), "Expected a binary result request");
    const request = fromBinary(GetQueryResultRequestSchema, payload);
    reads.push(request);
    const selected = reply;
    if (selected.kind === "held") { selected.capture(route, request); return; }
    const snapshot = selected.kind === "valid"
      ? validPage(selected.sample, expiresAt)
      : invalidPage(selected.sample, expiresAt);
    await fulfillPage(route, snapshot);
  });
  await page.goto(`/requests/${requestID}/result`);
  const panel = page.getByRole("region", { name: "Query results", exact: true });
  await expect(panel.getByRole("heading", { name: "Query result", exact: true })).toBeVisible();
  return { panel, reads, expiresAt, executionCalls: () => executionCalls, setReply: next => { reply = next; } };
}

const validSamples: { sample: ValidSample; expectedText: string }[] = [
  { sample: "exact", expectedText: "9007199254740993\t18446744073709551615\t-19.9500000000000000001" },
  { sample: "literals", expectedText: '""\tfalse\tNULL\tNULL' },
  { sample: "binary-floats", expectedText: "\\x00ff10\tNaN\tInfinity\t-Infinity" },
  { sample: "mixed-storage", expectedText: "storage-class-text\topaque-custom-value\t2026-10-24T00:00:00Z" },
];

for (const { sample, expectedText } of validSamples) {
  test(`valid ${sample} values retain their exact Table and Text representation`, async ({ page }) => {
    const workspace = await openWorkspace(page, { kind: "valid", sample });
    await expect(workspace.panel.getByText("Rows 1–1 of 1", { exact: true })).toBeVisible();
    await expect(workspace.panel.getByRole("alert")).toHaveCount(0);
    await workspace.panel.getByRole("button", { name: "Text", exact: true }).click();
    await expect(workspace.panel.getByLabel("Text results", { exact: true })).toContainText(expectedText);
    await workspace.panel.getByRole("button", { name: "Table", exact: true }).click();
    await expect(workspace.panel.getByRole("row")).toHaveCount(2);
    await expect(workspace.panel.getByRole("cell")).toHaveText(expectedText.split("\t").map(value => value === '""' ? "" : value));
    expect(workspace.reads.length).toBe(1);
    expect(workspace.executionCalls()).toBe(0);
  });
}

test("an empty validated result shows its headers without inventing NULL rows", async ({ page }) => {
  const workspace = await openWorkspace(page, { kind: "valid", sample: "empty" });
  await expect(workspace.panel.getByText("Rows 0–0 of 0", { exact: true })).toBeVisible();
  await workspace.panel.getByRole("button", { name: "Text", exact: true }).click();
  await expect(workspace.panel.getByLabel("Text results", { exact: true })).toHaveText("protected_value");
  await expect(workspace.panel.getByRole("alert")).toHaveCount(0);
  expect(workspace.executionCalls()).toBe(0);
});

test("a validated zero-column result remains an empty view without a validation error", async ({ page }) => {
  const workspace = await openWorkspace(page, { kind: "valid", sample: "zero-columns" });
  await expect(workspace.panel.getByText("Rows 0–0 of 0", { exact: true })).toBeVisible();
  await workspace.panel.getByRole("button", { name: "Text", exact: true }).click();
  await expect(workspace.panel.getByLabel("Text results", { exact: true })).toHaveText("");
  await expect(workspace.panel.getByRole("alert")).toHaveCount(0);
  expect(workspace.executionCalls()).toBe(0);
});

const invalidSamples: InvalidSample[] = ["unset-kind", "false-null", "unknown-enum", "missing-expiry", "invalid-expiry", "invalid-page", "wrong-row-width"];

for (const sample of invalidSamples) {
  test(`an invalid ${sample} response is refused before rendering with a sanitized retryable error`, async ({ page }) => {
    const workspace = await openWorkspace(page, { kind: "invalid", sample });
    await expect(workspace.panel.getByRole("alert")).toHaveText(invalidResponseMessage);
    await expect(workspace.panel.getByText(protectedText, { exact: true })).toHaveCount(0);
    await expect(workspace.panel.getByRole("cell")).toHaveCount(0);
    await expect(workspace.panel.getByRole("button", { name: "Retry result", exact: true })).toBeVisible();
    expect(workspace.executionCalls()).toBe(0);
  });
}

test("a corrected response retries without executing SQL or retaining the rejected view's cells", async ({ page }) => {
  const workspace = await openWorkspace(page, { kind: "valid", sample: "protected" });
  await expect(workspace.panel.getByText(protectedText, { exact: true })).toBeVisible();
  workspace.setReply({ kind: "invalid", sample: "unset-kind" });
  await workspace.panel.getByRole("button", { name: /protected_value/ }).click();
  await expect(workspace.panel.getByRole("alert")).toHaveText(invalidResponseMessage);
  await expect(workspace.panel.getByText(protectedText, { exact: true })).toHaveCount(0);
  workspace.setReply({ kind: "valid", sample: "protected" });
  const priorReads = workspace.reads.length;
  await workspace.panel.getByRole("button", { name: "Retry result", exact: true }).click();
  await expect(workspace.panel.getByText(protectedText, { exact: true })).toBeVisible();
  await expect(workspace.panel.getByRole("alert")).toHaveCount(0);
  expect(workspace.reads.length).toBe(priorReads + 1);
  expect(workspace.executionCalls()).toBe(0);
});

test("session metadata loss fences a delayed invalid response and recovers through a fresh read", async ({ page }) => {
  const pending: PendingResultRead = { current: { kind: "waiting" } };
  const workspace = await openWorkspace(page, { kind: "held", capture: route => { pending.current = { kind: "captured", route }; } });
  await expect.poll(() => pending.current.kind).toBe("captured");
  await page.route("**/portcullis.v1.Auth/Me", route => fulfillConnectError(route, "unavailable"));
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(page.getByRole("heading", { name: "Portcullis is temporarily unreachable" })).toBeVisible();
  await fulfillPage(requirePendingRoute(pending), invalidPage("unset-kind", workspace.expiresAt));
  await expect(page.getByText(invalidResponseMessage, { exact: true })).toHaveCount(0);
  await expect(page.getByText(protectedText, { exact: true })).toHaveCount(0);
  workspace.setReply({ kind: "valid", sample: "protected" });
  await page.unroute("**/portcullis.v1.Auth/Me");
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(workspace.panel.getByText(protectedText, { exact: true })).toBeVisible();
  await expect(workspace.panel.getByRole("alert")).toHaveCount(0);
  expect(workspace.executionCalls()).toBe(0);
});
