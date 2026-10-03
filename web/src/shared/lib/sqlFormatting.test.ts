import { describe, expect, it } from "vitest";
import { formatPostgresSQL } from "@/shared/lib/sqlFormatting";

describe("local PostgreSQL formatting", () => {
  it("indents a query while preserving named parameters, casts and exact literals", () => {
    const result = formatPostgresSQL("select :amount::numeric as total, $1::bigint as id from orders where note = 'MiXeD :secret' and id = :id -- keep this comment");
    expect(result.error).toBe("");
    expect(result.sql).toContain("\n  :amount::numeric");
    expect(result.sql).toContain("$1::bigint");
    expect(result.sql).toContain("'MiXeD :secret'");
    expect(result.sql).toContain(":id");
    expect(result.sql).toContain("-- keep this comment");
    expect(result.sql).not.toContain("SELECT");
    expect(formatPostgresSQL(result.sql).sql).toBe(result.sql);
  });
  it("keeps incomplete or unsupported SQL intact and reports a recoverable message", () => {
    const sql = "select 'unfinished";
    expect(formatPostgresSQL(sql)).toEqual({ sql, error: "SQL could not be formatted. Your input is unchanged." });
  });
  it("does not rewrite literal concatenation whose newline has meaning", () => {
    const sql = "select 'first'\n'second' as text";
    expect(formatPostgresSQL(sql).sql).toBe(sql);
    const invalid = "select 'first' 'second'";
    expect(formatPostgresSQL(invalid).sql).toBe(invalid);
    const comments = "select 'first' /* separator */\n'second'";
    expect(formatPostgresSQL(comments).sql).toBe(comments);
  });
  it("preserves dollar-quoted bodies and nested comments", () => {
    const result = formatPostgresSQL("select $body$MiXeD :name\n raw text$body$, 1 /* outer /* nested */ still outer */");
    expect(result.error).toBe("");
    expect(result.sql).toContain("$body$MiXeD :name\n raw text$body$");
    expect(result.sql).toContain("/* outer /* nested */ still outer */");
  });
  it("bounds formatter input and leaves empty SQL alone", () => {
    expect(formatPostgresSQL("")).toEqual({ sql: "", error: "" });
    const sql = "-- " + "界".repeat(23_000);
    expect(formatPostgresSQL(sql).sql).toBe(sql);
    expect(formatPostgresSQL(sql).error).not.toBe("");
  });
});
