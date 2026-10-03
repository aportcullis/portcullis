import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { CellValueSchema, QueryResultPageSchema, LogicalType } from "@/gen/portcullis/v1/query_executions_pb";
import { resultText, resultClipboard } from "@/features/request/resultPresentation";

const cell = (value: string) => create(CellValueSchema, { kind: { case: "stringValue", value } });

describe("visible result presentation", () => {
  it("copies headers and exact visible values in their current order without numeric coercion", () => {
    const page = create(QueryResultPageSchema, { columns: [{ name: "id", logicalType: LogicalType.INT }, { name: "amount", logicalType: LogicalType.DECIMAL }], rows: [{ cells: [cell("9007199254740993"), cell("-19.9500")] }], totalCount: 200n });
    expect(resultClipboard(page)).toBe("id\tamount\n9007199254740993\t-19.9500");
  });
  it("quotes tabs, line breaks and quotes, and makes formula-looking text safe for spreadsheet paste", () => {
    const page = create(QueryResultPageSchema, { columns: [{ name: "=header", logicalType: LogicalType.STRING }], rows: [{ cells: [cell("  =SUM(A1:A2)")] }, { cells: [cell('line\t"two"\nthree')] }] });
    expect(resultClipboard(page)).toBe('\'=header\n\'  =SUM(A1:A2)\n"line\t""two""\nthree"');
    expect(resultText(page)).toContain("  =SUM(A1:A2)");
    expect(resultText(page)).not.toContain("'=header");
  });
  it("retains NULL, empty strings and byte values as distinct displayed text", () => {
    const page = create(QueryResultPageSchema, { columns: [{ name: "value" }], rows: [{ cells: [create(CellValueSchema, { kind: { case: "isNull", value: true } })] }, { cells: [cell("")] }, { cells: [create(CellValueSchema, { kind: { case: "bytesValue", value: new Uint8Array([0, 255]) } })] }] });
    expect(resultText(page)).toBe('value\nNULL\n""\n\\x00ff');
  });
});

const spreadsheetCases = [
  { text: "\ntext", expected: "\"'\ntext\"" },
  { text: "\ttext", expected: "\"'\ttext\"" },
  { text: "\rtext", expected: "\"'\rtext\"" },
  { text: "＝1+1", expected: "'＝1+1" },
  { text: " ＋1", expected: "' ＋1" },
  { text: "－1", expected: "'－1" },
  { text: "＠SUM(A1:A2)", expected: "'＠SUM(A1:A2)" },
];
it.each(spreadsheetCases)("protects header and visible text $text without changing Text view", ({ text, expected }) => {
  const page = create(QueryResultPageSchema, { columns: [{ name: text, logicalType: LogicalType.STRING }], rows: [{ cells: [cell(text)] }] });
  expect(resultClipboard(page)).toBe(`${expected}\n${expected}`);
  expect(resultText(page)).not.toBe(resultClipboard(page));
});
