import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type {
  ColumnMeta,
  QueryResultPage,
  QueryResultRow,
} from "@/gen/portcullis/v1/query_executions_pb";

/** Limits each server result read to a fixed bounded page. */
export const RESULT_PAGE_SIZE = 50;
/** Limits retained server pages independently of rendered rows. */
export const RESULT_CACHE_PAGE_LIMIT = 5;
/** Limits conservative retained payload accounting in the browser. */
export const RESULT_CACHE_BYTE_LIMIT = 64 * 1024 * 1024;
/** Limits unsettled reads, including aborted readers awaiting settlement. */
export const RESULT_FETCH_LIMIT = 2;

/** Identifies an immutable result view without changing its execution. */
export type ResultWindowQuery = {
  requestId: string;
  sortColumn?: number;
  descending: boolean;
  filter: string;
};
/** Identifies one bounded server page within a result view. */
export type ResultWindowRead = ResultWindowQuery & {
  page: number;
  pageSize: number;
};
/** Classifies failures by whether protected data can remain available. */
export type ResultWindowFailure = "retryable" | "refused" | "unavailable";
/** Supplies server reads and lifecycle notifications to the result window. */
export type ResultWindowOptions = {
  readPage: (request: ResultWindowRead, signal: AbortSignal) => Promise<QueryResultPage>;
  onChange: () => void;
  classifyError?: (error: unknown) => ResultWindowFailure;
};
/** Reports metadata and retention measurements without duplicating retained rows. */
export type ResultWindowSnapshot = {
  generation: number;
  columns: readonly ColumnMeta[];
  totalCount: bigint;
  totalPages: number;
  truncated: boolean;
  expiresAt: Timestamp | undefined;
  retainedPages: number[];
  retainedBytes: number;
  inFlight: number;
  pendingPages: number[];
  loading: boolean;
  error: unknown | undefined;
  unavailable: boolean;
};
/** Owns one bounded server-page cache and rejects superseded reads. */
export type ResultWindowController = {
  reset: (query: ResultWindowQuery) => void;
  setRange: (firstRow: number, lastRow: number) => void;
  retry: () => void;
  clear: () => void;
  dispose: () => void;
  rowAt: (index: number) => QueryResultRow | undefined;
  snapshot: () => ResultWindowSnapshot;
};

/** Creates a result-window controller from a consumer-owned page reader. */
export function createResultWindow(options: ResultWindowOptions): ResultWindowController {
  const cachedPages = new Map<number, CachedResultPage>();
  const activeReads = new Map<number, ActiveResultRead>();
  const failedPages = new Set<number>();
  const suppressedPrefetchPages = new Set<number>();
  let generation = 0;
  let nextReadID = 0;
  let currentQuery: ResultWindowQuery | undefined;
  let metadata: ResultWindowMetadata | undefined;
  let firstRow = 0;
  let lastRow = 0;
  let error: unknown;
  let unavailable = false;
  let terminal = false;
  let disposed = false;
  let pumping = false;

  const notifyChange = () => {
    if (!disposed) options.onChange();
  };
  const visiblePages = (): number[] => {
    if (!metadata) return [1];
    const firstPage = Math.floor(firstRow / RESULT_PAGE_SIZE) + 1;
    const lastPage = Math.floor(lastRow / RESULT_PAGE_SIZE) + 1;
    return Array.from(
      { length: Math.min(3, lastPage - firstPage + 1) },
      (_unused, pageOffset) => firstPage + pageOffset,
    );
  };
  const desiredPages = (): number[] => {
    const visible = visiblePages();
    const firstPage = visible[0] ?? 1;
    const lastPage = visible.at(-1) ?? 1;
    return [
      ...visible,
      ...(firstPage > 1 ? [firstPage - 1] : []),
      ...(metadata && lastPage < metadata.totalPages ? [lastPage + 1] : []),
    ];
  };
  const hasActiveRead = (page: number) => [...activeReads.values()].some(
    read => read.generation === generation && read.page === page,
  );
  const pendingPages = () => !currentQuery || terminal ? [] : desiredPages().filter(
    page => !cachedPages.has(page) && !failedPages.has(page) && !hasActiveRead(page) &&
      (!suppressedPrefetchPages.has(page) || visiblePages().includes(page)),
  );
  const retainedBytes = () => [...cachedPages.values()].reduce(
    (total, page) => total + page.byteCount,
    metadata ? measureResultMetadataBytes(metadata.columns) : 0,
  );
  const abortReads = () => {
    for (const read of activeReads.values()) read.controller.abort();
  };
  const clearState = () => {
    generation++;
    abortReads();
    cachedPages.clear();
    failedPages.clear();
    suppressedPrefetchPages.clear();
    currentQuery = undefined;
    metadata = undefined;
    firstRow = 0;
    lastRow = 0;
    error = undefined;
    unavailable = false;
    terminal = false;
  };
  const isCurrentRead = (read: ActiveResultRead) => !disposed && !terminal &&
    read.generation === generation && !read.controller.signal.aborted &&
    desiredPages().includes(read.page);
  const refuseOutput = (failure: unknown, kind: ResultWindowFailure) => {
    clearState();
    error = failure;
    unavailable = kind === "unavailable";
    terminal = true;
  };
  const recordFailure = (read: ActiveResultRead, failure: unknown) => {
    if (!isCurrentRead(read)) return;
    const kind = options.classifyError?.(failure) ?? "retryable";
    if (kind !== "retryable") {
      refuseOutput(failure, kind);
      return;
    }
    failedPages.add(read.page);
    error = failure;
  };
  const retainPage = (read: ActiveResultRead, response: QueryResultPage) => {
    if (!isCurrentRead(read)) return;
    const expectedRowCount = Math.max(
      0,
      Math.min(RESULT_PAGE_SIZE, Number(response.totalCount) - (read.page - 1) * RESULT_PAGE_SIZE),
    );
    if (
      response.page !== read.page || response.pageSize !== RESULT_PAGE_SIZE ||
      response.rows.length !== expectedRowCount ||
      response.rows.some(row => row.cells.length !== response.columns.length) ||
      response.totalCount < 0n || response.totalCount > BigInt(Number.MAX_SAFE_INTEGER) ||
      response.totalPages !== Math.max(1, Math.ceil(Number(response.totalCount) / RESULT_PAGE_SIZE))
    ) {
      recordFailure(read, new Error("The result page does not match the requested range."));
      return;
    }
    if (metadata && !hasMatchingResultMetadata(metadata, response)) {
      refuseOutput(new Error("The result snapshot changed while it was being read."), "unavailable");
      return;
    }
    const byteCount = measureResultRowBytes(response.rows);
    const newMetadataBytes = metadata ? 0 : measureResultMetadataBytes(response.columns);
    if (byteCount + (metadata ? measureResultMetadataBytes(metadata.columns) : newMetadataBytes) > RESULT_CACHE_BYTE_LIMIT) {
      recordFailure(read, new Error("This result page exceeds the browser retention limit. Export CSV to inspect the snapshot."));
      return;
    }
    const protectedPages = visiblePages();
    const center = (protectedPages[0] ?? 1) + (protectedPages.at(-1) ?? 1);
    const evictionCandidates = [...cachedPages.keys()]
      .filter(page => !protectedPages.includes(page))
      .sort((leftPage, rightPage) => Math.abs(rightPage * 2 - center) - Math.abs(leftPage * 2 - center));
    while (
      cachedPages.size >= RESULT_CACHE_PAGE_LIMIT ||
      retainedBytes() + byteCount + newMetadataBytes > RESULT_CACHE_BYTE_LIMIT
    ) {
      const evictedPage = evictionCandidates.shift();
      if (evictedPage === undefined) {
        if (protectedPages.includes(read.page)) {
          failedPages.add(read.page);
          error = new Error("The visible result range exceeds the browser retention limit. Export CSV to inspect the snapshot.");
        } else suppressedPrefetchPages.add(read.page);
        return;
      }
      cachedPages.delete(evictedPage);
      if (desiredPages().includes(evictedPage)) suppressedPrefetchPages.add(evictedPage);
    }
    metadata ??= {
      columns: response.columns,
      totalCount: response.totalCount,
      totalPages: response.totalPages,
      truncated: response.truncated,
      expiresAt: response.expiresAt,
    };
    cachedPages.set(read.page, { rows: response.rows, byteCount });
  };
  const startPageRead = (page: number, query: ResultWindowQuery) => {
    const read: ActiveResultRead = { generation, page, controller: new AbortController() };
    const readID = ++nextReadID;
    activeReads.set(readID, read);
    let response: Promise<QueryResultPage>;
    try {
      response = options.readPage({ ...query, page, pageSize: RESULT_PAGE_SIZE }, read.controller.signal);
    } catch (failure) {
      response = Promise.reject(failure);
    }
    void response.then(
      value => retainPage(read, value),
      failure => recordFailure(read, failure),
    ).finally(() => {
      activeReads.delete(readID);
      pumpReads();
      notifyChange();
    });
  };
  const pumpReads = () => {
    if (pumping || disposed || terminal || !currentQuery) return;
    pumping = true;
    try {
      while (activeReads.size < RESULT_FETCH_LIMIT) {
        const nextPage = pendingPages()[0];
        if (nextPage === undefined) break;
        startPageRead(nextPage, currentQuery);
      }
    } finally {
      pumping = false;
    }
  };

  return {
    reset: query => {
      if (disposed) return;
      clearState();
      currentQuery = { ...query };
      pumpReads();
      notifyChange();
    },
    setRange: (requestedFirstRow, requestedLastRow) => {
      if (
        disposed || terminal || !currentQuery ||
        !Number.isSafeInteger(requestedFirstRow) || !Number.isSafeInteger(requestedLastRow)
      ) return;
      const maximumRow = Math.max(0, Number(metadata?.totalCount ?? 1n) - 1);
      const nextFirstRow = Math.min(maximumRow, Math.max(0, requestedFirstRow));
      const nextLastRow = Math.min(
        maximumRow,
        Math.max(nextFirstRow, requestedLastRow),
        nextFirstRow + RESULT_PAGE_SIZE * 2 - 1,
      );
      if (nextFirstRow === firstRow && nextLastRow === lastRow) return;
      firstRow = nextFirstRow;
      lastRow = nextLastRow;
      const desired = desiredPages();
      for (const read of activeReads.values())
        if (read.generation === generation && !desired.includes(read.page)) read.controller.abort();
      for (const page of failedPages)
        if (!desired.includes(page)) failedPages.delete(page);
      for (const page of suppressedPrefetchPages)
        if (!desired.includes(page)) suppressedPrefetchPages.delete(page);
      if (failedPages.size === 0) error = undefined;
      pumpReads();
      notifyChange();
    },
    retry: () => {
      if (disposed || terminal || !currentQuery) return;
      failedPages.clear();
      suppressedPrefetchPages.clear();
      error = undefined;
      pumpReads();
      notifyChange();
    },
    clear: () => {
      if (!disposed) {
        clearState();
        notifyChange();
      }
    },
    dispose: () => {
      if (!disposed) {
        clearState();
        disposed = true;
      }
    },
    rowAt: index => {
      if (!Number.isSafeInteger(index) || index < 0) return undefined;
      return cachedPages.get(Math.floor(index / RESULT_PAGE_SIZE) + 1)?.rows[index % RESULT_PAGE_SIZE];
    },
    snapshot: () => ({
      generation,
      columns: metadata?.columns ?? [],
      totalCount: metadata?.totalCount ?? 0n,
      totalPages: metadata?.totalPages ?? 0,
      truncated: metadata?.truncated ?? false,
      expiresAt: metadata?.expiresAt,
      retainedPages: [...cachedPages.keys()].sort((leftPage, rightPage) => leftPage - rightPage),
      retainedBytes: retainedBytes(),
      inFlight: activeReads.size,
      pendingPages: pendingPages(),
      loading: !terminal && visiblePages().some(page => hasActiveRead(page) || pendingPages().includes(page)),
      error,
      unavailable,
    }),
  };
}

type CachedResultPage = {
  rows: QueryResultRow[];
  byteCount: number;
};
type ActiveResultRead = {
  generation: number;
  page: number;
  controller: AbortController;
};
type ResultWindowMetadata = Pick<ResultWindowSnapshot, "columns" | "totalCount" | "totalPages" | "truncated" | "expiresAt">;

/** Accounts metadata independently of the page that first supplied it. */
function measureResultMetadataBytes(columns: readonly ColumnMeta[]): number {
  return columns.reduce(
    (byteCount, column) => byteCount + 128 + (column.name.length + column.dbTypeName.length) * 2,
    256,
  );
}

/** Accounts retained UTF-16 strings, binary buffers and conservative object overhead. */
function measureResultRowBytes(rows: QueryResultRow[]): number {
  let byteCount = 128;
  const countedBuffers = new Set<ArrayBufferLike>();
  for (const row of rows) {
    byteCount += 96;
    for (const cell of row.cells) {
      byteCount += 128;
      const value = cell.kind.value;
      if (typeof value === "string") byteCount += value.length * 2;
      else if (value instanceof Uint8Array && !countedBuffers.has(value.buffer)) {
        countedBuffers.add(value.buffer);
        byteCount += value.buffer.byteLength;
      }
      else byteCount += 8;
    }
  }
  return byteCount;
}

/** Verifies that independent page reads describe the same immutable snapshot. */
function hasMatchingResultMetadata(metadata: ResultWindowMetadata, page: QueryResultPage): boolean {
  return metadata.totalCount === page.totalCount && metadata.totalPages === page.totalPages &&
    metadata.truncated === page.truncated &&
    metadata.expiresAt?.seconds === page.expiresAt?.seconds && metadata.expiresAt?.nanos === page.expiresAt?.nanos &&
    metadata.columns.length === page.columns.length && metadata.columns.every((column, columnIdx) => {
      const receivedColumn = page.columns[columnIdx];
      return receivedColumn !== undefined && column.name === receivedColumn.name &&
        column.dbTypeName === receivedColumn.dbTypeName &&
        column.logicalType === receivedColumn.logicalType && column.nullable === receivedColumn.nullable;
    });
}
