import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { A } from "@solidjs/router";
import type { Component } from "solid-js";
import {
  For,
  Index,
  Show,
  createEffect,
  createMemo,
  createSignal,
  on,
  onCleanup,
  untrack,
} from "solid-js";
import { createSolidTable, getCoreRowModel } from "@tanstack/solid-table";

import type {
  ColumnMeta,
  QueryExecution,
  QueryResultPage,
  QueryResultRow,
} from "@/gen/portcullis/v1/query_executions_pb";
import {
  QueryResultPageSchema,
  QueryResultRowSchema,
  LogicalType,
} from "@/gen/portcullis/v1/query_executions_pb";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { errorMessage, isAuthorizationRefusal } from "@/shared/api/errors";
import { executionsClient } from "@/shared/api/client";
import { ExecutionSummary } from "@/features/request/ExecutionSummary";
import { describeResultError } from "@/features/request/resultErrors";
import { cellText, resultText, resultClipboard } from "@/features/request/resultPresentation";
import { cycleResultSorting } from "@/features/request/sorting";
import { createResultWindow } from "@/features/request/resultWindow";
import { LoadingSkeleton } from "@/shared/ui/LoadingSkeleton";
import { Button, buttonVariants } from "@/shared/ui/button";
import { nativeControlVariants } from "@/shared/ui/control";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/table";

const resultRowHeight = 40;
const resultOverscanRows = 4;

/** Selects the presentation of the visible result range. */
export type ResultDisplayMode = "table" | "text";
/** Defines the inputs owned by ResultPanel. */
export type ResultPanelProps = {
  requestId: string;
};

/** Displays bounded virtual rows from an owner-scoped snapshot without executing SQL again. */
export const ResultPanel: Component<ResultPanelProps> = (props) => {
  const [execution, setExecution] = createSignal<QueryExecution>();
  const [columns, setColumns] = createSignal<readonly ColumnMeta[]>([]);
  const [windowRevision, setWindowRevision] = createSignal(0);
  const [firstVisibleRow, setFirstVisibleRow] = createSignal(0);
  const [scrollOffset, setScrollOffset] = createSignal(0);
  const [bodyViewportHeight, setBodyViewportHeight] = createSignal(400);
  const [sortColumn, setSortColumn] = createSignal<number>();
  const [descending, setDescending] = createSignal(false);
  const [filter, setFilter] = createSignal("");
  const [filterDraft, setFilterDraft] = createSignal("");
  const [fullCell, setFullCell] = createSignal<string>();
  const [view, setView] = createSignal<ResultDisplayMode>("table");
  const [copying, setCopying] = createSignal(false);
  const [copyMessage, setCopyMessage] = createSignal("");
  const [initialLoading, setInitialLoading] = createSignal(false);
  const [initialError, setInitialError] = createSignal("");
  const [terminalUnavailable, setTerminalUnavailable] = createSignal(false);
  const [exporting, setExporting] = createSignal(false);
  const [exportError, setExportError] = createSignal("");
  const [downloadURL, setDownloadURL] = createSignal<string>();
  let revision = 0;
  let requestRevision = 0;
  let initialRead: AbortController | undefined;
  let viewport: HTMLDivElement | undefined;
  let copyInFlight = false;
  const discardDownload = () => {
    const url = downloadURL();
    if (url) URL.revokeObjectURL(url);
    setDownloadURL();
  };
  const windowController = createResultWindow({
    readPage: (request, signal) => executionsClient.getResult(request, { signal }),
    onChange: () => setWindowRevision(value => value + 1),
    classifyError: error => {
      if (isAuthorizationRefusal(error)) return "refused";
      if (
        error instanceof ConnectError &&
        [Code.FailedPrecondition, Code.NotFound].includes(error.code)
      ) return "unavailable";
      return "retryable";
    },
  });
  const snapshot = createMemo(() => {
    windowRevision();
    return windowController.snapshot();
  });
  const rowCount = () => Number(snapshot().totalCount);
  const visibleEnd = () => Math.min(
    rowCount(),
    Math.ceil((scrollOffset() + bodyViewportHeight()) / resultRowHeight),
  );
  const renderedStart = () => Math.max(0, firstVisibleRow() - resultOverscanRows);
  const renderedEnd = () => Math.min(rowCount(), visibleEnd() + resultOverscanRows);
  const renderedIndices = createMemo(() => Array.from(
    { length: Math.max(0, renderedEnd() - renderedStart()) },
    (_unused, offset) => renderedStart() + offset,
  ));
  const renderedRows = createMemo(() => {
    windowRevision();
    return Array.from(
      { length: Math.max(0, renderedEnd() - renderedStart()) },
      (_unused, offset) => windowController.rowAt(renderedStart() + offset) ?? create(QueryResultRowSchema),
    );
  });
  const visibleSnapshot = (): QueryResultPage => create(QueryResultPageSchema, {
    columns: [...columns()],
    rows: Array.from(
      { length: Math.max(0, visibleEnd() - firstVisibleRow()) },
      (_unused, offset) => windowController.rowAt(firstVisibleRow() + offset),
    ).filter((row): row is QueryResultRow => row !== undefined),
  });
  const visibleRowsReady = () => snapshot().totalPages > 0 && !terminalUnavailable() &&
    visibleSnapshot().rows.length === visibleEnd() - firstVisibleRow();
  const resultFailureMessage = (error: unknown) => describeResultError(
    error,
    failure => failure instanceof Error && !(failure instanceof ConnectError)
      ? failure.message
      : errorMessage(failure),
  );
  const currentError = () => initialError() || (
    snapshot().error === undefined ? "" : resultFailureMessage(snapshot().error)
  );
  const discardProtectedOutput = () => {
    revision++;
    requestRevision++;
    discardDownload();
    setFullCell();
    setCopying(false);
    setCopyMessage("");
    setExporting(false);
  };
  const discardUnavailableSnapshot = () => {
    if (terminalUnavailable()) return;
    discardProtectedOutput();
    setTerminalUnavailable(true);
    setColumns([]);
    setExecution(current => current ? { ...current, resultAvailable: false } : undefined);
  };
  const reloadResultWindow = () => {
    revision++;
    setFullCell();
    setCopying(false);
    setCopyMessage("");
    setInitialError("");
    setTerminalUnavailable(false);
    setFirstVisibleRow(0);
    setScrollOffset(0);
    if (viewport) viewport.scrollTop = 0;
    windowController.reset({
      requestId: props.requestId,
      sortColumn: sortColumn(),
      descending: descending(),
      filter: filter(),
    });
  };
  const loadExecution = async () => {
    initialRead?.abort();
    const controller = new AbortController();
    initialRead = controller;
    const captured = requestRevision;
    setInitialLoading(true);
    setInitialError("");
    try {
      const info = await executionsClient.get({ requestId: props.requestId }, { signal: controller.signal });
      if (controller.signal.aborted || captured !== requestRevision) return;
      setExecution(info);
      if (info.resultAvailable) reloadResultWindow();
    } catch (error) {
      if (controller.signal.aborted || captured !== requestRevision) return;
      setInitialError(resultFailureMessage(error));
      if (isAuthorizationRefusal(error)) {
        discardUnavailableSnapshot();
        setExecution();
      }
    } finally {
      if (!controller.signal.aborted) setInitialLoading(false);
    }
  };
  const applyResultFilter = (event: SubmitEvent) => {
    event.preventDefault();
    setFilter(filterDraft());
    reloadResultWindow();
  };
  const retryResultRead = () => {
    if (execution()?.resultAvailable) windowController.retry();
    else void loadExecution();
  };
  createEffect(on(() => props.requestId, () => {
    discardProtectedOutput();
    setTerminalUnavailable(false);
    setExportError("");
    windowController.clear();
    setExecution();
    setColumns([]);
    setSortColumn();
    setDescending(false);
    setFilter("");
    setFilterDraft("");
    setView("table");
    void loadExecution();
  }));
  createEffect(() => {
    const state = snapshot();
    if (state.columns.length > 0 && untrack(columns).length === 0) setColumns(state.columns);
    if (state.unavailable || isAuthorizationRefusal(state.error)) untrack(discardUnavailableSnapshot);
    if (isAuthorizationRefusal(state.error)) setExecution();
  });
  createEffect(() => {
    if (terminalUnavailable() || !execution()?.resultAvailable) return;
    const expiry = execution()?.resultExpiresAt ?? snapshot().expiresAt;
    if (!expiry) return;
    const remaining = Number(expiry.seconds) * 1000 + expiry.nanos / 1_000_000 - Date.now();
    const timer = setTimeout(() => {
      windowController.clear();
      discardUnavailableSnapshot();
      setInitialError("Results may have expired or been evicted.");
    }, Math.max(0, remaining));
    onCleanup(() => clearTimeout(timer));
  });
  createEffect(() => {
    const start = renderedStart();
    const end = renderedEnd();
    untrack(() => windowController.setRange(start, Math.max(start, end - 1)));
  });
  onCleanup(() => {
    initialRead?.abort();
    windowController.dispose();
    discardProtectedOutput();
  });
  const cycleColumnSorting = (column: number) => {
    const next = cycleResultSorting({ column: sortColumn(), descending: descending() }, column);
    setSortColumn(next.column);
    setDescending(next.descending);
    reloadResultWindow();
  };
  const updateVisibleRange = (element: HTMLDivElement) => {
    const start = Math.min(
      Math.max(0, rowCount() - 1),
      Math.floor(element.scrollTop / resultRowHeight),
    );
    const active = document.activeElement;
    if (active instanceof HTMLElement && element.contains(active) && active.dataset.resultRow !== undefined) {
      const focusedRow = Number(active.dataset.resultRow);
      if (
        focusedRow < Math.max(0, start - resultOverscanRows) ||
        focusedRow >= Math.ceil((element.scrollTop + bodyViewportHeight()) / resultRowHeight) + resultOverscanRows
      ) element.focus({ preventScroll: true });
    }
    if (element.scrollTop !== scrollOffset()) {
      revision++;
      setFullCell();
      setCopyMessage("");
      setCopying(false);
      setFirstVisibleRow(start);
      setScrollOffset(element.scrollTop);
    }
  };
  const observeResultViewport = (element: HTMLDivElement) => {
    viewport = element;
    const measureViewport = () => {
      if (element.clientHeight > 0)
        setBodyViewportHeight(Math.min(400, Math.max(1, element.clientHeight - 64)));
    };
    const observer = new ResizeObserver(measureViewport);
    observer.observe(element);
    measureViewport();
    onCleanup(() => observer.disconnect());
  };
  const navigateResultRows = (event: KeyboardEvent) => {
    if (event.target !== event.currentTarget || !(event.currentTarget instanceof HTMLDivElement)) return;
    const element = event.currentTarget;
    const maximum = Math.max(0, rowCount() * resultRowHeight - bodyViewportHeight());
    const offsets: Record<string, number> = {
      ArrowDown: element.scrollTop + resultRowHeight,
      ArrowUp: element.scrollTop - resultRowHeight,
      PageDown: element.scrollTop + bodyViewportHeight(),
      PageUp: element.scrollTop - bodyViewportHeight(),
      Home: 0,
      End: maximum,
    };
    const offset = offsets[event.key];
    if (offset === undefined) return;
    event.preventDefault();
    element.scrollTop = Math.min(maximum, Math.max(0, offset));
    updateVisibleRange(element);
  };
  const table = createSolidTable<QueryResultRow>({
    get data() {
      return renderedRows();
    },
    get columns() {
      return columns().map((column, columnIdx) => ({
        id: String(columnIdx),
        accessorFn: (row: QueryResultRow) => cellText(row.cells[columnIdx]),
        header: column.name,
      }));
    },
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    manualSorting: true,
    manualFiltering: true,
  });
  const copyVisibleRows = async () => {
    if (copyInFlight || !visibleRowsReady()) return;
    copyInFlight = true;
    const copiedSnapshot = visibleSnapshot();
    const captured = revision;
    const capturedRequest = requestRevision;
    setCopying(true);
    setCopyMessage("");
    try {
      if (!navigator.clipboard?.writeText) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(resultClipboard(copiedSnapshot));
      if (captured === revision && capturedRequest === requestRevision) setCopyMessage(`Copied ${copiedSnapshot.rows.length} visible rows with column headers.`);
    } catch {
      if (captured === revision && capturedRequest === requestRevision) setCopyMessage("Clipboard access was denied or unavailable. Select text in Text view or export CSV instead.");
    } finally {
      copyInFlight = false;
      if (captured === revision && capturedRequest === requestRevision) setCopying(false);
    }
  };
  // The export streams the whole snapshot regardless of the shown page, sort or filter, so only a newer export or another request supersedes it.
  let exportAttempt = 0;
  const exportCSV = async () => {
    const captured = requestRevision;
    const attempt = ++exportAttempt;
    const isCurrentExport = () => captured === requestRevision && attempt === exportAttempt;
    setExporting(true);
    setExportError("");
    discardDownload();
    try {
      const chunks: Uint8Array<ArrayBuffer>[] = [];
      for await (const chunk of executionsClient.exportCSV({ requestId: props.requestId ?? "" })) {
        if (!isCurrentExport()) return;
        chunks.push(new Uint8Array(chunk.data));
      }
      if (!isCurrentExport()) return;
      setDownloadURL(URL.createObjectURL(new Blob(chunks, { type: "text/csv;charset=utf-8" })));
    } catch (err) {
      if (isCurrentExport()) setExportError(describeResultError(err, errorMessage));
    } finally {
      if (isCurrentExport()) setExporting(false);
    }
  };
  return (
    <section aria-label="Query results" class="flex min-w-0 flex-col gap-6">
      <header>
        <A href={`/requests/${props.requestId}`} class="text-sm underline">
          Back to request
        </A>
        <h1 class="mt-3 text-2xl font-semibold">
          Query result
        </h1>
      </header>
      <Show when={execution()}>
        {info => <ExecutionSummary execution={info()} />}
      </Show>
      <Show when={initialLoading()}>
        <LoadingSkeleton label="Loading result…" />
      </Show>
      <Show when={currentError()}>
        <div>
          <p role="alert" class="text-destructive-text">
            {currentError()}
          </p>
          <Show when={!terminalUnavailable() && !snapshot().unavailable && !isAuthorizationRefusal(snapshot().error)}>
            <Button variant="outline" size="sm" onClick={retryResultRead}>
              Retry result
            </Button>
          </Show>
        </div>
      </Show>
      <Show when={execution()?.state === AccessRequestState.OUTCOME_UNKNOWN}>
        <p role="alert">
          Outcome unknown. Check the target database and audit history before creating another request. This execution will not retry.
        </p>
      </Show>
      <Show when={execution()?.state === AccessRequestState.FAILED}>
        <p role="alert">Execution failed. Submit a new request to try again.</p>
      </Show>
      <Show when={execution()?.state === AccessRequestState.SUCCEEDED && !execution()?.resultAvailable}>
        <p>
          Execution succeeded. The cached result is no longer available.
        </p>
      </Show>
      <Show when={execution()?.resultAvailable}>
        <Show when={snapshot().truncated}>
          <p role="status">Result truncated by the row or byte limit. CSV contains only this cached snapshot.</p>
        </Show>
        <p class="text-xs text-muted-foreground">
          Results expire after 15 minutes and may be evicted earlier.
        </p>
        <div class="content-surface result-controls">
          <form
            class="result-filter"
            onSubmit={applyResultFilter}
          >
            <label class="result-search-label">
              Search results
              <input
                aria-label="Filter results"
                class={nativeControlVariants({ size: "sm", class: "w-full" })}
                value={filterDraft()}
                maxLength={1000}
                onInput={(event) => setFilterDraft(event.currentTarget.value)}
                placeholder="Search all columns"
              />
            </label>
            <Button type="submit" size="sm">
              Filter results
            </Button>
          </form>
          <div role="group" aria-label="Result actions" class="result-actions">
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={copying() || !visibleRowsReady()}
              onClick={() => void copyVisibleRows()}
            >
              Copy visible rows
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={exporting()}
              onClick={() => void exportCSV()}
            >
              Export CSV
            </Button>
            <Show when={downloadURL()}>
              {(url) => (
                <a
                  class={buttonVariants({ size: "sm" })}
                  href={url()}
                  download="query-result.csv"
                >
                  Download CSV
                </a>
              )}
            </Show>
            <Show when={exportError()}>
              <p role="alert" class="basis-full text-sm text-destructive-text">
                CSV export failed: {exportError()}
              </p>
            </Show>
          </div>
        </div>
        <p role="status" class="text-sm text-muted-foreground">
          <Show when={sortColumn() !== undefined} fallback="Original query order.">
            Sorted by {columns()[sortColumn() ?? 0]?.name}{" "}
            {descending() ? "descending" : "ascending"} across the cached snapshot. NULL values
            last.
          </Show>
        </p>
        <p id="result-sorting-help" class="sr-only">
          Column buttons cycle ascending, descending and original query order. Sorting changes
          only the cached result view; CSV uses original query order.
        </p>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div
            role="group"
            aria-label="Result view"
            class="inline-flex gap-1 rounded-lg border bg-card p-1"
          >
            <Button
              type="button"
              size="sm"
              variant={view() === "table" ? "default" : "ghost"}
              aria-pressed={view() === "table"}
              onClick={() => setView("table")}
            >
              Table
            </Button>
            <Button
              type="button"
              size="sm"
              variant={view() === "text" ? "default" : "ghost"}
              aria-pressed={view() === "text"}
              onClick={() => setView("text")}
            >
              Text
            </Button>
          </div>
          <p class="text-xs text-muted-foreground">
            Copy includes the labeled visible row range only; formula-like text is escaped for spreadsheet paste.
          </p>
        </div>
        <Show when={copyMessage()}>
          <p role="status" class="text-sm text-muted-foreground">
            {copyMessage()}
          </p>
        </Show>
        <Show when={snapshot().loading}>
          <p role="status" aria-busy="true">Loading result rows…</p>
        </Show>
        <p role="status" aria-label="Visible result range" class="text-sm">
          Rows {rowCount() === 0 ? 0 : firstVisibleRow() + 1}–{visibleEnd()} of {snapshot().totalCount.toString()}
        </p>
        <p id="result-scroll-help" class="sr-only">
          Focus the result viewport and use Arrow keys, Page Up, Page Down, Home or End to explore rows. Text and Copy visible rows include only the labeled row range.
        </p>
        <div classList={{ hidden: view() !== "table" }}>
          <div
            ref={observeResultViewport}
            role="region"
            aria-label="Scrollable result rows"
            aria-describedby="result-scroll-help"
            tabindex="0"
            class="result-viewport rounded-lg border"
            data-retained-pages={snapshot().retainedPages.length}
            data-retained-bytes={snapshot().retainedBytes}
            data-in-flight={snapshot().inFlight}
            onScroll={event => updateVisibleRange(event.currentTarget)}
            onKeyDown={navigateResultRows}
          >
            <Table class="result-table" aria-describedby="result-sorting-help" aria-rowcount={rowCount() + 1} aria-colcount={columns().length}>
              <TableHeader class="sticky top-0 z-10 bg-card">
                <TableRow>
                  <Index each={columns()}>
                    {(column, index) => (
                      <TableHead
                        role="columnheader"
                        scope="col"
                        class="h-16"
                        aria-sort={
                          sortColumn() === index
                            ? descending()
                              ? "descending"
                              : "ascending"
                            : undefined
                        }
                      >
                        <button
                          type="button"
                          class="h-16 w-full overflow-hidden whitespace-nowrap rounded px-1 py-1 text-left hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring"
                          title={
                            sortColumn() !== index
                              ? "Sort ascending"
                              : descending()
                                ? "Restore query order"
                                : "Sort descending"
                          }
                          onClick={() => cycleColumnSorting(index)}
                        >
                          {column().name}
                          <Show when={sortColumn() === index}>
                            <span aria-hidden="true" class="ml-1">
                              {descending() ? "↓" : "↑"}
                            </span>
                          </Show>
                          <span class="block text-xs text-muted-foreground">
                            {LogicalType[column().logicalType]} · {column().dbTypeName}
                          </span>
                        </button>
                      </TableHead>
                    )}
                  </Index>
                </TableRow>
              </TableHeader>
              <TableBody>
                <Show when={renderedStart() > 0}>
                  <TableRow aria-hidden="true" class="border-0">
                    <TableCell colspan={columns().length} class="p-0" style={{ height: `${renderedStart() * resultRowHeight}px` }} />
                  </TableRow>
                </Show>
                <For each={renderedIndices()}>
                  {rowIndex => {
                    const row = () => table.getRowModel().rows[rowIndex - renderedStart()];
                    const rowReady = () => (row()?.original.cells.length ?? 0) > 0;
                    const displayedCell = (columnIndex: number) => String(row()?.getValue(String(columnIndex)) ?? "NULL");
                    return (
                      <TableRow aria-rowindex={rowIndex + 2} class="h-10">
                        <Index each={columns()}>
                          {(_column, columnIndex) => (
                            <TableCell class="h-10 p-0">
                              <button
                                type="button"
                                title="View full cell"
                                data-result-row={rowIndex}
                                disabled={!rowReady()}
                                class="block h-10 w-full truncate px-4 text-left font-mono text-xs hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring"
                                onClick={() => setFullCell(displayedCell(columnIndex))}
                              >
                                {rowReady() ? displayedCell(columnIndex) : "Loading…"}
                              </button>
                            </TableCell>
                          )}
                        </Index>
                      </TableRow>
                    );
                  }}
                </For>
                <Show when={renderedEnd() < rowCount()}>
                  <TableRow aria-hidden="true" class="border-0">
                    <TableCell colspan={columns().length} class="p-0" style={{ height: `${(rowCount() - renderedEnd()) * resultRowHeight}px` }} />
                  </TableRow>
                </Show>
              </TableBody>
            </Table>
          </div>
        </div>
        <Show when={view() === "text"}>
          <pre aria-label="Text results" class="result-text max-h-[36rem] overflow-auto rounded-lg border bg-card p-4 font-mono text-sm leading-6">
            {visibleRowsReady() ? resultText(visibleSnapshot()) : "Loading visible rows…"}
          </pre>
        </Show>
        <Show when={fullCell() !== undefined}>
          <div>
            <Button variant="ghost" onClick={() => setFullCell()}>
              Close cell
            </Button>
            <pre aria-label="Full cell" class="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded border p-3">
              {fullCell()}
            </pre>
          </div>
        </Show>
      </Show>
    </section>
  );
};
