import { describe, expect, it } from "vitest";

import { cycleResultSorting } from "@/features/request/sorting";

describe("result sort selection", () => {
  it("cycles the same column through ascending, descending and original query order", () => {
    const ascending = cycleResultSorting({ column: undefined, descending: false }, 2);
    expect(ascending).toEqual({ column: 2, descending: false });
    const descending = cycleResultSorting(ascending, 2);
    expect(descending).toEqual({ column: 2, descending: true });
    expect(cycleResultSorting(descending, 2)).toEqual({ column: undefined, descending: false });
  });
  it("starts a different column in ascending order", () => {
    expect(cycleResultSorting({ column: 2, descending: true }, 1)).toEqual({ column: 1, descending: false });
  });
});
