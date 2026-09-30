import { describe, expect, it } from "vitest";

import { truncateReasonCodePoints, countReasonCodePoints } from "@/entities/request/reason";


describe("approval reason length", () => {
  const emoji = "🙂";
  const limit = 1000;

  it("counts code points, not UTF-16 code units", () => {
    expect(countReasonCodePoints(emoji)).toBe(1);
    expect(emoji.length).toBe(2); // what maxlength would have counted
    expect(countReasonCodePoints("abc")).toBe(3);
    expect(countReasonCodePoints("")).toBe(0);
  });

  it("accepts a reason of exactly the limit in emoji", () => {
    const value = emoji.repeat(limit);
    expect(countReasonCodePoints(value)).toBe(limit);
    expect(truncateReasonCodePoints(value, limit)).toBe(value);
  });

  it("clamps one code point past the limit, keeping whole characters", () => {
    const clamped = truncateReasonCodePoints(emoji.repeat(limit + 1), limit);
    expect(countReasonCodePoints(clamped)).toBe(limit);
    // Never a broken surrogate pair: the tail must still be a whole emoji.
    expect(clamped.endsWith(emoji)).toBe(true);
  });

  it("clamps ASCII at the same boundary", () => {
    expect(truncateReasonCodePoints("r".repeat(limit), limit)).toHaveLength(limit);
    expect(truncateReasonCodePoints("r".repeat(limit + 1), limit)).toHaveLength(limit);
  });

  it("counts a combining sequence as its code points, matching the server", () => {
    // e + U+0301 renders as one grapheme but is two code points, and both the Go and PostgreSQL checks count two. Counting graphemes here would let a reason through that the server then refuses.
    const combining = "é";
    expect(countReasonCodePoints(combining)).toBe(2);
    expect(truncateReasonCodePoints(combining, 1)).toBe("e");
  });

  it("leaves the value alone when the limit is unknown", () => {

    const value = emoji.repeat(5);
    expect(truncateReasonCodePoints(value, undefined)).toBe(value);
  });
});
