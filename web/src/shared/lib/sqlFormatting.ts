import { formatDialect, postgresql } from "sql-formatter";

const maxFormatBytes = 64 * 1024;

/** Formats editable PostgreSQL without substituting parameters; failures retain the original input. */
export function formatPostgresSQL(sql: string): { sql: string; error: string } {
  if (sql.trim() === "") return { sql, error: "" };
  if (sql.length > maxFormatBytes || new TextEncoder().encode(sql).length > maxFormatBytes) {
    return { sql, error: "SQL is too large to format. Your input is unchanged." };
  }
  // PostgreSQL concatenates adjacent string literals only when their separator includes a newline.
  if (/'(?:\s|--[^\n]*(?:\n|$)|\/\*[\s\S]*?\*\/)+'/.test(sql)) {
    return { sql, error: "SQL with adjacent string literals is left unchanged to preserve its meaning." };
  }
  try {
    return { sql: formatDialect(sql, {
      dialect: postgresql, tabWidth: 2, keywordCase: "preserve", identifierCase: "preserve",
      functionCase: "preserve", dataTypeCase: "preserve", paramTypes: { named: [":"], numbered: ["$"] },
    }), error: "" };
  } catch {
    return { sql, error: "SQL could not be formatted. Your input is unchanged." };
  }
}
