import { describe, expect, it } from "vitest";
import { z } from "zod";

import { isValidationError, parseValidatedValue, ValidationError } from "@/shared/lib/validation";

/** Checks that a refused boundary reveals no source content or issue metadata. */
function expectSanitizedFailure(operation: () => unknown): void {
  try {
    operation();
  } catch (error) {
    expect(isValidationError(error)).toBe(true);
    if (!isValidationError(error)) throw error;
    expect(error.message).toBe("The response is invalid.");
    expect(error.name).toBe("ValidationError");
    expect("issues" in error).toBe(false);
    expect("cause" in error).toBe(false);
    expect(JSON.stringify(error)).not.toContain("private-sql-value");
    expect(String(error)).not.toContain("private-sql-value");
    return;
  }
  throw new Error("Expected boundary refusal.");
}

describe("shared runtime validation", () => {
  it("returns typed object output while stripping unknown fields", () => {
    const schema = z.object({ count: z.number(), label: z.string() });
    const parsed: z.output<typeof schema> = parseValidatedValue(schema, { count: 2, label: "exact", extra: "drop" });
    expect(parsed).toEqual({ count: 2, label: "exact" });
  });

  it("preserves empty text, false and big integers without coercion", () => {
    const schema = z.object({ text: z.string(), enabled: z.boolean(), count: z.bigint() });
    expect(parseValidatedValue(schema, { text: "", enabled: false, count: 9007199254740993n }))
      .toEqual({ text: "", enabled: false, count: 9007199254740993n });
  });

  it("types transforming schemas by their output without claiming a generic input guard", () => {
    const schema = z.string().transform(value => value.length);
    const length: number = parseValidatedValue(schema, "exact");
    expect(length).toBe(5);
  });

  it("preserves discriminated-union output and identifies only sanitized errors", () => {
    const schema = z.discriminatedUnion("kind", [
      z.object({ kind: z.literal("ready"), value: z.string() }),
      z.object({ kind: z.literal("absent") }),
    ]);
    expect(parseValidatedValue(schema, { kind: "absent" })).toEqual({ kind: "absent" });
    expect(isValidationError(new ValidationError())).toBe(true);
    expect(isValidationError(new Error("other"))).toBe(false);
  });

  it("sanitizes rejected scalar values", () => {
    expectSanitizedFailure(() => parseValidatedValue(z.number(), "private-sql-value"));
  });

  it("sanitizes custom issue messages", () => {
    const schema = z.string().refine(() => false, { error: "private-sql-value" });
    expectSanitizedFailure(() => parseValidatedValue(schema, "private-sql-value"));
  });

  it("sanitizes exceptions thrown by a refinement", () => {
    const schema = z.string().refine(() => { throw new Error("private-sql-value"); });
    expectSanitizedFailure(() => parseValidatedValue(schema, "private-sql-value"));
  });

  it("sanitizes exceptions thrown while reading a boundary object", () => {
    const schema = z.object({ value: z.string() });
    const boundary = { get value(): string { throw new Error("private-sql-value"); } };
    expectSanitizedFailure(() => parseValidatedValue(schema, boundary));
  });
});
