import { create } from "@bufbuild/protobuf";
import { describe, expect, it, vi } from "vitest";

import {
  CellValueSchema,
  LogicalType,
  QueryResultPageSchema,
} from "@/gen/portcullis/v1/query_executions_pb";
import type { QueryResultPage } from "@/gen/portcullis/v1/query_executions_pb";
import {
  assertQueryResultPage,
  createValidatedQueryResultReader,
  isQueryResultPage,
  parseQueryResultPage,
  QueryResultValidationError,
} from "@/shared/api/queryResultValidation";
import { isValidationError } from "@/shared/lib/validation";

/** Supplies a complete generated result value with a required protobuf expiry. */
function resultPage(): QueryResultPage {
  return create(QueryResultPageSchema, {
    columns: [{ name: "value", dbTypeName: "text", logicalType: LogicalType.STRING, nullable: true }],
    rows: [{ cells: [{ kind: { case: "stringValue", value: "exact" } }] }],
    page: 1,
    pageSize: 10,
    totalCount: 1n,
    totalPages: 1,
    truncated: false,
    expiresAt: { seconds: 1792800000n, nanos: 123 },
  });
}

/** Requires a first cell in a fixture whose row and column order is the scenario. */
function requireFirstCell(page: QueryResultPage) {
  for (const { cells } of page.rows) for (const cell of cells) return cell;
  throw new Error("Expected a fixture cell.");
}

describe("validated query result values", () => {
  it("preserves exact text, false, SQL NULL, binary and nonfinite float values", () => {
    const values = [
      create(CellValueSchema, { kind: { case: "stringValue", value: "" } }),
      create(CellValueSchema, { kind: { case: "boolValue", value: false } }),
      create(CellValueSchema, { kind: { case: "intValue", value: "9007199254740993" } }),
      create(CellValueSchema, { kind: { case: "decimalValue", value: "-19.9500" } }),
      create(CellValueSchema, { kind: { case: "isNull", value: true } }),
      create(CellValueSchema, { kind: { case: "bytesValue", value: new Uint8Array([0, 255]) } }),
      create(CellValueSchema, { kind: { case: "doubleValue", value: Number.NaN } }),
      create(CellValueSchema, { kind: { case: "doubleValue", value: Infinity } }),
      create(CellValueSchema, { kind: { case: "doubleValue", value: -Infinity } }),
    ];
    const page = resultPage();
    page.rows = values.map(cell => ({ $typeName: "portcullis.v1.QueryResultRow", cells: [cell] }));
    page.totalCount = BigInt(values.length);
    expect(parseQueryResultPage(page).rows).toEqual(page.rows);
    expect(isQueryResultPage(page)).toBe(true);
    expect(() => assertQueryResultPage(page)).not.toThrow();
  });

  it("preserves opaque decimal and temporal strings and empty column labels", () => {
    for (const text of ["NaN", "Infinity", "-Infinity", "  001.2300", "unusual temporal value"]) {
      const page = resultPage();
      for (const column of page.columns) column.name = "";
      requireFirstCell(page).kind = { case: "decimalValue", value: text };
      expect(requireFirstCell(parseQueryResultPage(page)).kind.value).toBe(text);
      requireFirstCell(page).kind = { case: "temporalValue", value: text };
      expect(requireFirstCell(parseQueryResultPage(page)).kind.value).toBe(text);
    }
  });

  it("accepts each server page size, empty snapshots and zero-column rows", () => {
    for (const pageSize of [10, 20, 50, 100]) {
      const page = resultPage();
      page.pageSize = pageSize;
      page.columns = [];
      page.rows = [];
      page.totalCount = 0n;
      expect(parseQueryResultPage(page).totalCount).toBe(0n);
      page.rows = [{ $typeName: "portcullis.v1.QueryResultRow", cells: [] }];
      page.totalCount = 1n;
      expect(parseQueryResultPage(page).rows).toHaveLength(1);
    }
  });

  it("strips unknown fields and detaches tiny and empty binary backing buffers", () => {
    for (const length of [0, 2]) {
      const backing = new Uint8Array(1024 * 1024);
      backing.fill(65);
      const page = resultPage();
      const cell = requireFirstCell(page);
      cell.kind = { case: "bytesValue", value: backing.subarray(0, length) };
      const input = Object.assign(page, { $unknown: [{ data: backing }], extra: backing });
      Object.assign(cell, { $unknown: [{ data: backing }], extra: backing });
      Object.assign(cell.kind, { extra: backing });
      const parsed = parseQueryResultPage(input);
      expect("$unknown" in parsed).toBe(false);
      expect("extra" in parsed).toBe(false);
      const parsedCell = requireFirstCell(parsed);
      expect("$unknown" in parsedCell).toBe(false);
      expect("extra" in parsedCell.kind).toBe(false);
      if (parsedCell.kind.case !== "bytesValue") throw new Error("Expected binary output.");
      expect(parsedCell.kind.value.byteLength).toBe(length);
      expect(parsedCell.kind.value.buffer.byteLength).toBe(length);
      expect(parsedCell.kind.value.buffer).not.toBe(backing.buffer);
      expect("$unknown" in input).toBe(true);
    }
  });
});

describe("refused query result boundaries", () => {
  it("rejects unset or false NULL discriminators instead of presenting invented NULL", () => {
    const page = resultPage();
    for (const { cells } of page.rows) cells.splice(0, 1, create(CellValueSchema));
    expect(() => parseQueryResultPage(page)).toThrow(QueryResultValidationError);
    requireFirstCell(page).kind = { case: "isNull", value: false };
    expect(() => assertQueryResultPage(page)).toThrow(QueryResultValidationError);
  });

  it("rejects unsupported logical types while preserving the explicit UNKNOWN bucket", () => {
    const page = resultPage();
    for (const column of page.columns) column.logicalType = LogicalType.UNKNOWN;
    expect(parseQueryResultPage(page).columns).toEqual(page.columns);
    for (const logicalType of [0, 99]) {
      const columns = page.columns.map(column => ({ ...column, logicalType }));
      expect(isQueryResultPage({ ...page, columns })).toBe(false);
    }
  });

  it("rejects missing expiry, invalid protobuf time and incorrect row widths", () => {
    const page = resultPage();
    const { expiresAt: _expiresAt, ...missingExpiry } = page;
    expect(() => parseQueryResultPage(missingExpiry)).toThrow(QueryResultValidationError);
    expect(() => parseQueryResultPage({ ...page, expiresAt: { $typeName: "google.protobuf.Timestamp", seconds: 0n, nanos: 1000000000 } })).toThrow(QueryResultValidationError);
    expect(() => parseQueryResultPage({ ...page, rows: [{ $typeName: "portcullis.v1.QueryResultRow", cells: [] }] })).toThrow(QueryResultValidationError);
  });

  it("rejects invalid page bounds and incomplete pages without exposing response data", () => {
    const page = resultPage();
    const malformed = [
      { ...page, page: 0 },
      { ...page, pageSize: 11 },
      { ...page, totalCount: 10001n },
      { ...page, totalPages: 2 },
      { ...page, totalCount: 2n },
      { ...page, rows: [{ $typeName: "portcullis.v1.QueryResultRow", cells: [{ $typeName: "portcullis.v1.CellValue", kind: { case: "intValue", value: "private-sql-value" } }] }], page: -1 },
    ];
    for (const input of malformed) {
      try { parseQueryResultPage(input); }
      catch (error) {
        expect(isValidationError(error)).toBe(true);
        if (!isValidationError(error)) throw error;
        expect(error.message).toBe("The result response is invalid.");
        expect(String(error)).not.toContain("private-sql-value");
        continue;
      }
      throw new Error("Expected invalid result refusal.");
    }
  });

  it("validates exactly once after the injected API getter and propagates transport failures", async () => {
    const getter = vi.fn(async (_request: string, _signal: AbortSignal): Promise<unknown> => resultPage());
    const read = createValidatedQueryResultReader(getter);
    const controller = new AbortController();
    expect(await read("request-1", controller.signal)).toEqual(resultPage());
    expect(getter).toHaveBeenCalledWith("request-1", controller.signal);
    const failure = new Error("transport unavailable");
    getter.mockRejectedValueOnce(failure);
    await expect(read("request-1", controller.signal)).rejects.toBe(failure);
    getter.mockResolvedValueOnce({ private: "private-sql-value" });
    await expect(read("request-1", controller.signal)).rejects.toThrow(QueryResultValidationError);
  });
});
