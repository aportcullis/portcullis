import { describe, expect, it } from "vitest";

import { parseMaxResultMiB } from "@/features/connection/policyLimits";

// The number input reports partial or fractional entries as text; silently keeping the previous limit would save a value the admin did not see.
const mebibyte = 1048576n;

describe("parseMaxResultMiB", () => {
  it.each([
    ["16", 16n * mebibyte],
    ["1", mebibyte],
    ["64", 64n * mebibyte],
    [" 8 ", 8n * mebibyte],
    ["032", 32n * mebibyte],
  ])("converts %j MiB into bytes", (raw, bytes) => {
    expect(parseMaxResultMiB(raw)).toEqual({ status: "ok", bytes });
  });

  it("asks for a value when the field is empty", () => {
    expect(parseMaxResultMiB("")).toEqual({ status: "invalid", message: "Enter the max result size in MiB." });
  });

  it.each(["1.5", "abc", "-2", "1e3", "0", "0x10", "+4"])("refuses %j as not a positive whole number", (raw) => {
    expect(parseMaxResultMiB(raw)).toEqual({ status: "invalid", message: "Max result must be a positive whole number of MiB." });
  });

  it("refuses a value too large to be a safe integer instead of rounding it", () => {
    expect(parseMaxResultMiB("9007199254740993").status).toBe("invalid");
  });
});
