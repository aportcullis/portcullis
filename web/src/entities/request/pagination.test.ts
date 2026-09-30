import { describe, expect, it } from "vitest";

import { rangeEnd, rangeStart } from "@/entities/request/pagination";

describe("pagination range", () => {
  it("is 0/0 for an empty list", () => {
    expect(rangeStart(1, 20, 0n)).toBe(0);
    expect(rangeEnd(1, 20, 0, 0n)).toBe(0);
  });

  it("labels the first page from 1", () => {
    expect(rangeStart(1, 20, 25n)).toBe(1);
    expect(rangeEnd(1, 20, 20, 25n)).toBe(20);
  });

  it("keys the start off pageSize, not the current page's item count", () => {

    expect(rangeStart(2, 20, 25n)).toBe(21);
    expect(rangeEnd(2, 20, 5, 25n)).toBe(25);
  });
});
