import type { CellValue, QueryResultPage } from "@/gen/portcullis/v1/query_executions_pb";
import { LogicalType } from "@/gen/portcullis/v1/query_executions_pb";

/** Formats a wire cell without coercing exact integers or decimals to JavaScript numbers. */
export function cellText(cell?: CellValue): string {
  if (!cell || cell.kind.case === "isNull" || cell.kind.case === undefined) return "NULL";
  if (cell.kind.case === "bytesValue") return "\\x" + Array.from(cell.kind.value, b => b.toString(16).padStart(2, "0")).join("");
  return String(cell.kind.value);
}

const quoteTSV = (text: string): string => text === "" || /[\t\r\n"]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
const numericTypes = new Set([LogicalType.INT, LogicalType.DECIMAL, LogicalType.FLOAT]);
const numericText = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/;
const spreadsheetLiteral = (text: string, type?: LogicalType): string => {
  if (type !== undefined && numericTypes.has(type) && numericText.test(text)) return text;
  // Leading control characters must not bypass spreadsheet formula escaping.
  // eslint-disable-next-line no-control-regex
  return /^[\s\u0000-\u0020\u0085]*[=+\-@＝＋－＠\t\r\n]/.test(text) ? "'" + text : text;
};

const serialize = (page: QueryResultPage, spreadsheet: boolean): string => {
  const headers = page.columns.map(column => quoteTSV(spreadsheet ? spreadsheetLiteral(column.name) : column.name));
  const rows = page.rows.map(row => page.columns.map((column, index) => {
    const text = cellText(row.cells[index]);
    return quoteTSV(spreadsheet ? spreadsheetLiteral(text, column.logicalType) : text);
  }).join("\t"));
  return [headers.join("\t"), ...rows].join("\n");
};

/** Serializes headers and only the visible result page as tab-separated text. */
export function resultText(page: QueryResultPage): string { return serialize(page, false); }
/** Serializes only visible rows with spreadsheet-safe literals, preserving exact numeric text. */
export function resultClipboard(page: QueryResultPage): string { return serialize(page, true); }
