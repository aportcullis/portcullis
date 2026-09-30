/** Returns the first item's one-based index, or zero for an empty list. */
export function rangeStart(page: number, pageSize: number, totalCount: bigint): number {
  if (totalCount === 0n) return 0;
  return (page - 1) * pageSize + 1;
}

/** Returns the last displayed item's one-based index, or zero for an empty page. */
export function rangeEnd(page: number, pageSize: number, itemsOnPage: number, totalCount: bigint): number {
  if (totalCount === 0n || itemsOnPage === 0) return 0;
  return rangeStart(page, pageSize, totalCount) + itemsOnPage - 1;
}
