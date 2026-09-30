/** Counts reason length in Unicode code points. */
export function countReasonCodePoints(value: string): number {
  return Array.from(value).length;
}

/** Truncates to the code-point limit, leaving the text unchanged when no limit is supplied. */
export function truncateReasonCodePoints(value: string, limit: number | undefined): string {
  if (limit === undefined) return value;
  const points = Array.from(value);
  return points.length <= limit ? value : points.slice(0, limit).join("");
}
