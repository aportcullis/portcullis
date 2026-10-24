/** Holds the selected snapshot column and sort direction. */
export type ResultSorting = { column: number | undefined; descending: boolean };

/** Cycles a column between ascending, descending and original query order. */
export function cycleResultSorting(current: ResultSorting, column: number): ResultSorting {
  if (current.column !== column) return { column, descending: false };
  if (!current.descending) return { column, descending: true };
  return { column: undefined, descending: false };
}
