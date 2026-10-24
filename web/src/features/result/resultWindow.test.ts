import { create } from "@bufbuild/protobuf";
import { describe, expect, it, vi } from "vitest";

import {
  CellValueSchema,
  ColumnMetaSchema,
  QueryResultPageSchema,
  QueryResultRowSchema,
} from "@/gen/portcullis/v1/query_executions_pb";
import type { QueryResultPage } from "@/gen/portcullis/v1/query_executions_pb";
import {
  createResultWindow,
  RESULT_CACHE_BYTE_LIMIT,
  RESULT_CACHE_PAGE_LIMIT,
  RESULT_FETCH_LIMIT,
  RESULT_PAGE_SIZE,
} from "@/features/result/resultWindow";
import type { ResultWindowRead } from "@/features/result/resultWindow";

/** Provides explicitly scheduled read outcomes for request interleavings. */
function deferredPage() {
  let resolve = (_value: QueryResultPage): void => {
    throw new Error("Missing resolver");
  };
  let reject = (_reason: unknown): void => {
    throw new Error("Missing rejection");
  };
  const promise = new Promise<QueryResultPage>((settle, fail) => {
    resolve = settle;
    reject = fail;
  });
  return { promise, resolve, reject };
}

/** Builds exact wire values from the requested snapshot row range. */
function resultPage(request: ResultWindowRead, totalCount = 1000, contents?: string): QueryResultPage {
  const firstRow = (request.page - 1) * RESULT_PAGE_SIZE;
  return create(QueryResultPageSchema, {
    page: request.page,
    pageSize: RESULT_PAGE_SIZE,
    totalCount: BigInt(totalCount),
    totalPages: Math.max(1, Math.ceil(totalCount / RESULT_PAGE_SIZE)),
    columns: [create(ColumnMetaSchema, { name: "exact_value", dbTypeName: "int8" })],
    rows: Array.from(
      { length: Math.max(0, Math.min(RESULT_PAGE_SIZE, totalCount - firstRow)) },
      (_unused, rowOffset) => create(QueryResultRowSchema, {
        cells: [create(CellValueSchema, {
          kind: {
            case: "stringValue",
            value: contents ?? String(9007199254740993n + BigInt(firstRow + rowOffset)),
          },
        })],
      }),
    ),
  });
}

const query = { requestId: "request-1", descending: false, filter: "" };
const noChange = () => {};

/** Creates a bounded read descriptor for deferred fixture completion. */
function queryRead(page: number): ResultWindowRead {
  return {
    ...query,
    page,
    pageSize: RESULT_PAGE_SIZE,
  };
}

describe("bounded result navigation", () => {
  it("reads fixed pages and prefetches its nearby page while preserving exact values", async () => {
    const readPage = vi.fn(async (request: ResultWindowRead) => resultPage(request));
    const window = createResultWindow({ readPage, onChange: noChange });
    window.reset(query);
    await vi.waitFor(() => expect(window.snapshot().retainedPages).toEqual([1, 2]));
    expect(readPage.mock.calls.every(([request]) => request.pageSize === 50)).toBe(true);
    expect(window.rowAt(0)?.cells[0]?.kind.value).toBe("9007199254740993");
    expect(window.snapshot().retainedBytes).toBeGreaterThan(0);
    expect(window.snapshot().totalCount).toBe(1000n);
    window.dispose();
  });

  it("keeps the server's empty-result page without scheduling nonexistent neighbors", async () => {
    const readPage = vi.fn(async (request: ResultWindowRead) => resultPage(request, 0));
    const window = createResultWindow({ readPage, onChange: noChange });
    window.reset(query);
    await vi.waitFor(() => expect(window.snapshot().retainedPages).toEqual([1]));
    expect(window.snapshot().totalCount).toBe(0n);
    expect(window.snapshot().totalPages).toBe(1);
    expect(window.snapshot().columns[0]?.name).toBe("exact_value");
    expect(window.snapshot().error).toBeUndefined();
    expect(window.snapshot().loading).toBe(false);
    expect(readPage).toHaveBeenCalledTimes(1);
    window.dispose();
  });

  it("evicts distant pages and refetches an earlier range from the same execution", async () => {
    const readPage = vi.fn(async (request: ResultWindowRead) => resultPage(request));
    const window = createResultWindow({ readPage, onChange: noChange });
    window.reset(query);
    await vi.waitFor(() => expect(window.rowAt(0)).toBeDefined());
    for (const firstRow of [100, 250, 400, 600, 800]) {
      window.setRange(firstRow, firstRow + 17);
      await vi.waitFor(() => expect(window.rowAt(firstRow)).toBeDefined());
      expect(window.snapshot().retainedPages.length).toBeLessThanOrEqual(RESULT_CACHE_PAGE_LIMIT);
      expect(window.snapshot().retainedBytes).toBeLessThanOrEqual(RESULT_CACHE_BYTE_LIMIT);
    }
    expect(window.rowAt(0)).toBeUndefined();
    window.setRange(0, 17);
    await vi.waitFor(() => expect(window.rowAt(0)).toBeDefined());
    expect(readPage.mock.calls.filter(([request]) => request.page === 1)).toHaveLength(2);
    expect(readPage.mock.calls.every(([request]) => request.requestId === query.requestId)).toBe(true);
    window.dispose();
  });

  it("prioritizes all viewport pages before neighboring prefetch", async () => {
    const readPage = vi.fn(async (request: ResultWindowRead) => resultPage(request));
    const window = createResultWindow({ readPage, onChange: noChange });
    window.reset(query);
    await vi.waitFor(() => expect(window.rowAt(0)).toBeDefined());
    await vi.waitFor(() => expect(window.snapshot().inFlight).toBe(0));
    readPage.mockClear();
    window.setRange(248, 265);
    await vi.waitFor(() => expect(window.rowAt(265)).toBeDefined());
    expect(readPage.mock.calls.slice(0, 2).map(([request]) => request.page)).toEqual([5, 6]);
    await vi.waitFor(() => expect(window.snapshot().retainedPages).toContain(4));
    await vi.waitFor(() => expect(window.snapshot().retainedPages).toContain(7));
    window.dispose();
  });

  it("measures UTF-16 content and evicts distant pages before the byte limit", async () => {
    const largeValue = "값".repeat(9 * 1024 * 1024);
    const readPage = vi.fn(async (request: ResultWindowRead) => {
      const page = resultPage(request);
      const firstCell = page.rows[0]?.cells[0];
      if (firstCell) firstCell.kind = { case: "stringValue", value: largeValue };
      return page;
    });
    const window = createResultWindow({ readPage, onChange: noChange });
    window.reset(query);
    await vi.waitFor(() => expect(window.snapshot().retainedPages).toHaveLength(2));
    expect(window.snapshot().retainedBytes).toBeGreaterThan(36 * 1024 * 1024);
    window.setRange(200, 217);
    await vi.waitFor(() => expect(window.rowAt(200)).toBeDefined());
    await vi.waitFor(() => expect(window.snapshot().inFlight).toBe(0));
    expect(window.snapshot().retainedBytes).toBeLessThanOrEqual(RESULT_CACHE_BYTE_LIMIT);
    expect(window.snapshot().retainedPages.length).toBeLessThanOrEqual(3);
    window.dispose();
  });

  it("loads a previously skipped prefetch when that page enters the viewport", async () => {
    const largeValue = "x".repeat(12 * 1024 * 1024);
    const readPage = vi.fn(async (request: ResultWindowRead) => {
      const page = resultPage(request);
      const firstCell = page.rows[0]?.cells[0];
      if (firstCell) firstCell.kind = { case: "stringValue", value: largeValue };
      return page;
    });
    const window = createResultWindow({ readPage, onChange: noChange });
    window.reset(query);
    await vi.waitFor(() => expect(window.snapshot().retainedPages).toHaveLength(2));
    window.setRange(100, 167);
    await vi.waitFor(() => expect(window.snapshot().retainedPages).toEqual([3, 4]));
    await vi.waitFor(() => expect(window.snapshot().inFlight).toBe(0));
    expect(window.snapshot().error).toBeUndefined();
    window.setRange(200, 217);
    await vi.waitFor(() => expect(window.rowAt(200)).toBeDefined());
    expect(window.snapshot().retainedBytes).toBeLessThanOrEqual(RESULT_CACHE_BYTE_LIMIT);
    window.dispose();
  });
});

describe("result-window refusal and scheduled read races", () => {
  it("refuses changed snapshot columns rather than mixing incompatible cached pages", async () => {
    const window = createResultWindow({
      readPage: async request => {
        const page = resultPage(request);
        if (request.page === 3 && page.columns[0]) page.columns[0].dbTypeName = "text";
        return page;
      }, onChange: noChange,
    });
    window.reset(query);
    await vi.waitFor(() => expect(window.rowAt(0)).toBeDefined());
    window.setRange(100, 117);
    await vi.waitFor(() => expect(window.snapshot().unavailable).toBe(true));
    expect(window.snapshot().retainedPages).toEqual([]);
    window.dispose();
  });

  it("refuses an incomplete page rather than leaving unexplained holes in a loaded range", async () => {
    const window = createResultWindow({
      readPage: async request => {
        const page = resultPage(request);
        page.rows.pop();
        return page;
      }, onChange: noChange,
    });
    window.reset(query);
    await vi.waitFor(() => expect(window.snapshot().error).toBeInstanceOf(Error));
    expect(window.snapshot().retainedPages).toEqual([]);
    window.dispose();
  });

  it("bounds live reads and replaces pending ranges during rapid distant scrolling", async () => {
    const held: Array<{ request: ResultWindowRead; signal: AbortSignal; page: ReturnType<typeof deferredPage> }> = [];
    const window = createResultWindow({
      readPage: (request, signal) => {
        const page = deferredPage();
        held.push({ request, signal, page });
        return page.promise;
      }, onChange: noChange,
    });
    window.reset(query);
    held[0]?.page.resolve(resultPage(queryRead(1)));
    await vi.waitFor(() => expect(held).toHaveLength(2));
    window.setRange(400, 417);
    window.setRange(800, 817);
    window.setRange(600, 617);
    expect(window.snapshot().inFlight).toBeLessThanOrEqual(RESULT_FETCH_LIMIT);
    expect(window.snapshot().pendingPages.length).toBeLessThanOrEqual(4);
    expect(window.snapshot().pendingPages.every(page => page >= 12 && page <= 14)).toBe(true);
    for (const entry of held.slice(1)) entry.page.resolve(resultPage(entry.request));
    await vi.waitFor(() => expect(held.some(entry => entry.request.page === 13)).toBe(true));
    for (const entry of held) entry.page.resolve(resultPage(entry.request));
    await vi.waitFor(() => expect(window.rowAt(600)).toBeDefined());
    expect(window.snapshot().retainedPages).not.toContain(9);
    window.dispose();
  });

  it("aborts reset reads and ignores late old-view outcomes", async () => {
    const stale = deferredPage();
    let oldSignal: AbortSignal | undefined;
    const readPage = vi.fn((request: ResultWindowRead, signal: AbortSignal) => {
      if (request.filter === "old") { oldSignal = signal; return stale.promise; }
      return Promise.resolve(resultPage(request, 1, "new-view"));
    });
    const window = createResultWindow({ readPage, onChange: noChange });
    window.reset({ ...query, filter: "old" });
    window.reset({ ...query, filter: "new" });
    expect(oldSignal?.aborted).toBe(true);
    await vi.waitFor(() => expect(window.rowAt(0)?.cells[0]?.kind.value).toBe("new-view"));
    stale.resolve(resultPage({ ...queryRead(1), filter: "old" }, 1, "stale-view"));
    await Promise.resolve();
    expect(window.rowAt(0)?.cells[0]?.kind.value).toBe("new-view");
    window.dispose();
  });

  it("retains valid rows on transient failure and explicitly retries the failed page", async () => {
    const failed = new Set([3]);
    const failure = new Error("temporarily unavailable");
    const readPage = vi.fn(async (request: ResultWindowRead) => {
      if (failed.has(request.page)) throw failure;
      return resultPage(request);
    });
    const window = createResultWindow({ readPage, onChange: noChange });
    window.reset(query);
    await vi.waitFor(() => expect(window.rowAt(0)).toBeDefined());
    window.setRange(100, 117);
    await vi.waitFor(() => expect(window.snapshot().error).toBe(failure));
    expect(window.rowAt(0)).toBeDefined();
    expect(readPage.mock.calls.filter(([request]) => request.page === 3)).toHaveLength(1);
    failed.clear();
    window.retry();
    await vi.waitFor(() => expect(window.rowAt(100)).toBeDefined());
    expect(window.snapshot().error).toBeUndefined();
    window.dispose();
  });

  it("discards all cached rows and aborts siblings when access is refused", async () => {
    const refused = new Error("access lost");
    const held = deferredPage();
    let siblingSignal: AbortSignal | undefined;
    const window = createResultWindow({
      readPage: async (request, signal) => {
        if (request.page === 3) throw refused;
        if (request.page === 4) { siblingSignal = signal; return held.promise; }
        return resultPage(request);
      }, classifyError: error => error === refused ? "refused" : "retryable", onChange: noChange,
    });
    window.reset(query);
    await vi.waitFor(() => expect(window.rowAt(0)).toBeDefined());
    window.setRange(100, 167);
    await vi.waitFor(() => expect(window.snapshot().error).toBe(refused));
    expect(window.snapshot().retainedPages).toEqual([]);
    expect(window.snapshot().columns).toEqual([]);
    expect(window.rowAt(0)).toBeUndefined();
    expect(siblingSignal?.aborted).toBe(true);
    window.retry();
    held.resolve(resultPage(queryRead(4)));
    await Promise.resolve();
    expect(window.snapshot().retainedPages).toEqual([]);
    window.dispose();
  });

  it("marks an expired snapshot unavailable without retaining its earlier rows", async () => {
    const expired = new Error("snapshot expired");
    const window = createResultWindow({
      readPage: async request => {
        if (request.page > 2) throw expired;
        return resultPage(request);
      },
      classifyError: error => error === expired ? "unavailable" : "retryable", onChange: noChange,
    });
    window.reset(query);
    await vi.waitFor(() => expect(window.rowAt(0)).toBeDefined());
    window.setRange(100, 117);
    await vi.waitFor(() => expect(window.snapshot().unavailable).toBe(true));
    expect(window.snapshot().retainedBytes).toBe(0);
    expect(window.rowAt(0)).toBeUndefined();
    window.dispose();
  });

  it("refuses an oversized page instead of exceeding its retained-byte budget", async () => {
    const window = createResultWindow({
      readPage: async request => {
        const page = resultPage(request, 1);
        const firstCell = page.rows[0]?.cells[0];
        if (firstCell) firstCell.kind = { case: "stringValue", value: "x".repeat(RESULT_CACHE_BYTE_LIMIT / 2) };
        return page;
      }, onChange: noChange,
    });
    window.reset(query);
    await vi.waitFor(() => expect(window.snapshot().error).toBeInstanceOf(Error));
    expect(window.snapshot().retainedBytes).toBe(0);
    expect(window.rowAt(0)).toBeUndefined();
    expect(window.snapshot().inFlight).toBe(0);
    window.dispose();
  });

  it("disposes pending reads and prevents their callbacks from repopulating state", async () => {
    const held = deferredPage();
    const onChange = vi.fn();
    let signal: AbortSignal | undefined;
    const window = createResultWindow({
      readPage: (_request, requestSignal) => {
        signal = requestSignal;
        return held.promise;
      }, onChange,
    });
    window.reset(query);
    window.dispose();
    expect(signal?.aborted).toBe(true);
    const notifications = onChange.mock.calls.length;
    held.resolve(resultPage(queryRead(1)));
    await Promise.resolve();
    expect(window.snapshot().retainedPages).toEqual([]);
    expect(onChange).toHaveBeenCalledTimes(notifications);
  });
});
