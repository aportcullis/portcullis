import { z } from "zod";

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type { CellValue, ColumnMeta, QueryResultPage, QueryResultRow } from "@/gen/portcullis/v1/query_executions_pb";
import { LogicalType } from "@/gen/portcullis/v1/query_executions_pb";
import { parseValidatedValue, ValidationError } from "@/shared/lib/validation";

/** Receives one decoded result response through a consumer-owned API getter. */
export type QueryResultPageGetter<Request> = (request: Request, signal: AbortSignal) => Promise<unknown>;
/** Returns one validated result response before it enters browser retention. */
export type QueryResultPageReader<Request> = (request: Request, signal: AbortSignal) => Promise<QueryResultPage>;

const maxSnapshotRows = 10000;
const maxPageRows = 100;
const timestampSchema: z.ZodType<Timestamp> = z.object({
  $typeName: z.literal("google.protobuf.Timestamp"),
  seconds: z.bigint().min(-62135596800n).max(253402300799n),
  nanos: z.number().int().min(0).max(999999999),
});
const logicalTypeSchema = z.union([
  z.literal(LogicalType.STRING),
  z.literal(LogicalType.BOOL),
  z.literal(LogicalType.INT),
  z.literal(LogicalType.DECIMAL),
  z.literal(LogicalType.FLOAT),
  z.literal(LogicalType.BYTES),
  z.literal(LogicalType.DATE),
  z.literal(LogicalType.TIME),
  z.literal(LogicalType.TIMESTAMP),
  z.literal(LogicalType.TIMESTAMPTZ),
  z.literal(LogicalType.JSON),
  z.literal(LogicalType.UUID),
  z.literal(LogicalType.ARRAY),
  z.literal(LogicalType.UNKNOWN),
]);
const columnSchema: z.ZodType<ColumnMeta> = z.object({
  $typeName: z.literal("portcullis.v1.ColumnMeta"),
  name: z.string(),
  logicalType: logicalTypeSchema,
  dbTypeName: z.string(),
  nullable: z.boolean(),
});
const floatValueSchema = z.union([z.number(), z.nan(), z.literal(Infinity), z.literal(-Infinity)]);
const cellSchema: z.ZodType<CellValue> = z.object({
  $typeName: z.literal("portcullis.v1.CellValue"),
  kind: z.discriminatedUnion("case", [
    z.object({ case: z.literal("isNull"), value: z.literal(true) }),
    z.object({ case: z.literal("stringValue"), value: z.string() }),
    z.object({ case: z.literal("boolValue"), value: z.boolean() }),
    z.object({ case: z.literal("intValue"), value: z.string() }),
    z.object({ case: z.literal("decimalValue"), value: z.string() }),
    z.object({ case: z.literal("doubleValue"), value: floatValueSchema }),
    z.object({ case: z.literal("bytesValue"), value: z.instanceof(Uint8Array) }),
    z.object({ case: z.literal("temporalValue"), value: z.string() }),
  ]),
});
const rowSchema: z.ZodType<QueryResultRow> = z.object({
  $typeName: z.literal("portcullis.v1.QueryResultRow"),
  cells: z.array(cellSchema),
});

/** Describes the nontransforming result wire contract and strips unknown object fields. */
export const queryResultPageSchema: z.ZodType<QueryResultPage> = z.object({
  $typeName: z.literal("portcullis.v1.QueryResultPage"),
  columns: z.array(columnSchema),
  rows: z.array(rowSchema).max(maxPageRows),
  page: z.number().int().min(1).max(maxSnapshotRows),
  pageSize: z.union([z.literal(10), z.literal(20), z.literal(50), z.literal(100)]),
  totalCount: z.bigint().min(0n).max(BigInt(maxSnapshotRows)),
  totalPages: z.number().int().min(1).max(maxSnapshotRows),
  truncated: z.boolean(),
  expiresAt: timestampSchema,
}).refine(hasConsistentQueryResultPage);

/** Reports malformed result data without exposing response values or schema issues. */
export class QueryResultValidationError extends ValidationError {
  constructor() {
    super();
    this.name = "QueryResultValidationError";
    this.message = "The result response is invalid.";
  }
}

/** Parses decoded result data before it can enter a result window. */
export function parseQueryResultPage(input: unknown): QueryResultPage {
  try {
    const page: QueryResultPage = parseValidatedValue(queryResultPageSchema, input);
    for (const { cells } of page.rows) {
      for (const cell of cells) {
        if (cell.kind.case === "bytesValue") {
          const { value } = cell.kind;
          cell.kind.value = new Uint8Array(value);
        }
      }
    }
    return page;
  } catch {
    throw new QueryResultValidationError();
  }
}

/** Checks the untransformed result wire shape without claiming a generic schema guard. */
export function isQueryResultPage(input: unknown): input is QueryResultPage {
  try {
    return queryResultPageSchema.safeParse(input).success;
  } catch {
    return false;
  }
}

/** Refuses malformed result wire data at an explicit runtime assertion boundary. */
export function assertQueryResultPage(input: unknown): asserts input is QueryResultPage {
  if (!isQueryResultPage(input)) throw new QueryResultValidationError();
}

/** Adapts one decoded API getter into a validated result reader. */
export function createValidatedQueryResultReader<Request>(getter: QueryResultPageGetter<Request>): QueryResultPageReader<Request> {
  return async (request: Request, signal: AbortSignal): Promise<QueryResultPage> => parseQueryResultPage(await getter(request, signal));
}

/** Checks snapshot page arithmetic and positional row widths without changing values. */
function hasConsistentQueryResultPage(page: QueryResultPage): boolean {
  const { totalCount, pageSize, totalPages, rows, columns } = page;
  const expectedPages: number = Math.max(1, Math.ceil(Number(totalCount) / pageSize));
  const expectedRows: number = Math.max(0, Math.min(pageSize, Number(totalCount) - (page.page - 1) * pageSize));
  return totalPages === expectedPages && page.page <= totalPages && rows.length === expectedRows &&
    rows.every(({ cells }) => cells.length === columns.length);
}
