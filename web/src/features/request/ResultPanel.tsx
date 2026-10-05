import { A } from "@solidjs/router";
import type { Component } from "solid-js";
import { For, Show, createEffect, createMemo, createSignal, on, onCleanup } from "solid-js";
import { createSolidTable, getCoreRowModel } from "@tanstack/solid-table";

import type { QueryExecution, QueryResultPage, QueryResultRow } from "@/gen/portcullis/v1/query_executions_pb";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { LogicalType } from "@/gen/portcullis/v1/query_executions_pb";
import { errorMessage } from "@/shared/api/errors";
import { executionsClient } from "@/shared/api/client";
import { createOpenFetch } from "@/shared/lib/openFetch";
import { ExecutionSummary } from "@/features/request/ExecutionSummary";
import { describeResultError } from "@/features/request/resultErrors";
import { cellText, resultText, resultClipboard } from "@/features/request/resultPresentation";
import { cycleResultSorting } from "@/features/request/sorting";
import { LoadingSkeleton } from "@/shared/ui/LoadingSkeleton";
import { Button } from "@/shared/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/table";

/** Displays an owner-scoped result snapshot with server paging, sorting, filtering, and streamed export. */
export const ResultPanel: Component<{ requestId: string }> = (props) => {
  const [execution, setExecution] = createSignal<QueryExecution>();
  const [result, setResult] = createSignal<QueryResultPage>();
  const [page, setPage] = createSignal(1);
  const [pageSize, setPageSize] = createSignal(20);
  const [sortColumn, setSortColumn] = createSignal<number>();
  const [descending, setDescending] = createSignal(false);
  const [filter, setFilter] = createSignal("");
  const [filterDraft, setFilterDraft] = createSignal("");
  const [fullCell, setFullCell] = createSignal<string>();
  const [view, setView] = createSignal<"table" | "text">("table");
  const [copying, setCopying] = createSignal(false);
  const [copyMessage, setCopyMessage] = createSignal("");
  const [exporting, setExporting] = createSignal(false);
  // Export failures report beside the export controls; they say nothing about whether the shown page loaded.
  const [exportError, setExportError] = createSignal("");
  const [downloadURL, setDownloadURL] = createSignal<string>();
  const discardDownload = () => {
    const url = downloadURL();
    if (url) URL.revokeObjectURL(url);
    setDownloadURL();
  };
  onCleanup(discardDownload);
  let revision = 0;
  // requestRevision changes only when another request is shown; view reloads leave it alone.
  let requestRevision = 0;
  // The controls change before a read, so a refused read returns them to the view whose rows are still shown.
  type ResultView = { page: number; pageSize: number; sortColumn: number | undefined; descending: boolean; filter: string };
  const requestedView = (): ResultView => ({ page: page(), pageSize: pageSize(), sortColumn: sortColumn(), descending: descending(), filter: filter() });
  let appliedView: ResultView | undefined;
  const restoreView = (view: ResultView) => {
    setPage(view.page); setPageSize(view.pageSize); setSortColumn(view.sortColumn); setDescending(view.descending); setFilter(view.filter); setFilterDraft(view.filter);
  };
  const read = createOpenFetch(async () => {
    const view = requestedView();
    const info = await executionsClient.get({ requestId: props.requestId ?? "" });
    const snapshot = info.resultAvailable ? await executionsClient.getResult({
      requestId: props.requestId ?? "", page: view.page, pageSize: view.pageSize, sortColumn: view.sortColumn, descending: view.descending, filter: view.filter,
    }) : undefined;
    return { info, snapshot, view };
  }, ({ info, snapshot, view }) => { setExecution(info); setResult(snapshot); appliedView = view; }, (err) => describeResultError(err, errorMessage));
  createEffect(on(read.error, (message) => {
    if (message !== "" && appliedView !== undefined && result() !== undefined) restoreView(appliedView);
  }, { defer: true }));
  const id = createMemo(() => props.requestId);
  createEffect(on(id, (next) => {
    revision++;
    requestRevision++;
    appliedView = undefined;
    discardDownload();
    read.handleOpenChange(false);
    setExecution(); setResult(); setFullCell(); setExporting(false); setExportError(""); setView("table"); setCopying(false); setCopyMessage("");
    setPage(1); setPageSize(20); setSortColumn(); setDescending(false); setFilter(""); setFilterDraft("");
    if (next !== undefined) read.handleOpenChange(true);
  }));
  // A page, size, sort or filter change keeps the shown snapshot until its replacement arrives; createOpenFetch drops every response but the latest request's.
  const reload = () => { revision++; setCopyMessage(""); setCopying(false); setFullCell(); read.handleOpenChange(true); };
  const refreshing = () => read.loading() && result() !== undefined;
  const applySorting = (column: number | undefined, sortDescending: boolean) => {
    setSortColumn(column);
    setDescending(sortDescending);
    setPage(1);
    reload();
  };
  const cycleColumnSorting = (column: number) => {
    const next = cycleResultSorting({ column: sortColumn(), descending: descending() }, column);
    applySorting(next.column, next.descending);
  };
  onCleanup(() => { revision++; read.handleOpenChange(false); });
  const table = createSolidTable<QueryResultRow>({
    get data() { return result()?.rows ?? []; },
    get columns() { return (result()?.columns ?? []).map((column, columnIdx) => ({ id: String(columnIdx), accessorFn: (row: QueryResultRow) => cellText(row.cells[columnIdx]), header: column.name })); },
    getCoreRowModel: getCoreRowModel(), manualPagination: true, manualSorting: true, manualFiltering: true,
  });
  const copyVisibleRows = async () => {
    const snapshot = result();
    if (!snapshot || copying()) return;
    const captured = revision;
    const same = read.captureSession();
    setCopying(true); setCopyMessage("");
    try {
      if (!navigator.clipboard?.writeText) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(resultClipboard(snapshot));
      if (same() && captured === revision) setCopyMessage(`Copied ${snapshot.rows.length} visible rows with column headers.`);
    } catch {
      if (same() && captured === revision) setCopyMessage("Clipboard access was denied or unavailable. Select text in Text view or export CSV instead.");
    } finally { if (same() && captured === revision) setCopying(false); }
  };
  // The export streams the whole snapshot regardless of the shown page, sort or filter, so only a newer export or another request supersedes it.
  let exportAttempt = 0;
  const exportCSV = async () => {
    const captured = requestRevision;
    const attempt = ++exportAttempt;
    const isCurrentExport = () => captured === requestRevision && attempt === exportAttempt;
    setExporting(true); setExportError("");
    discardDownload();
    try {
      const chunks: Uint8Array<ArrayBuffer>[] = [];
      for await (const chunk of executionsClient.exportCSV({ requestId: props.requestId ?? "" })) {
        if (!isCurrentExport()) return;
        chunks.push(new Uint8Array(chunk.data));
      }
      if (!isCurrentExport()) return;
      setDownloadURL(URL.createObjectURL(new Blob(chunks, { type: "text/csv;charset=utf-8" })));
    } catch (err) { if (isCurrentExport()) setExportError(describeResultError(err, errorMessage)); }
    finally { if (isCurrentExport()) setExporting(false); }
  };
  return <section aria-label="Query results" class="flex min-w-0 flex-col gap-6">
      <header><A href={`/requests/${props.requestId}`} class="text-sm underline">Back to request</A>
      <h1 class="mt-3 text-2xl font-semibold">Query result</h1></header>
      <Show when={execution()}>{shownExecution => <ExecutionSummary execution={shownExecution()} />}</Show>
      <Show when={read.loading() && result() === undefined}><LoadingSkeleton label="Loading result…" /></Show>
      <Show when={read.error()}><p role="alert" class="text-destructive">{read.error()}</p></Show>
      <Show when={execution()?.state === AccessRequestState.OUTCOME_UNKNOWN}><p role="alert">Outcome unknown. Check the target database and audit history before creating another request. This execution will not retry.</p></Show>
      <Show when={execution()?.state === AccessRequestState.FAILED}><p role="alert">Execution failed. Submit a new request to try again.</p></Show>
      <Show when={execution()?.state === AccessRequestState.SUCCEEDED && !execution()?.resultAvailable}><p>Execution succeeded. The cached result is no longer available.</p></Show>
      <Show when={result()}>{snapshot => <>
        <Show when={snapshot().truncated}><p role="status">Result truncated by the row or byte limit. CSV contains only this cached snapshot.</p></Show>
        <p class="text-xs text-muted-foreground">Results expire after 15 minutes and may be evicted earlier.</p>
        <div class="content-surface result-controls">
        <form class="result-filter" onSubmit={event => { event.preventDefault(); setFilter(filterDraft()); setPage(1); reload(); }}>
          <label class="result-search-label">Search results
          <input aria-label="Filter results" class="result-control" value={filterDraft()} maxLength={1000} onInput={event => setFilterDraft(event.currentTarget.value)} placeholder="Search all columns" />
          </label>
          <Button type="submit">Filter results</Button>
        </form>
        <div role="group" aria-label="Result actions" class="result-actions">
          <Button type="button" variant="outline" disabled={copying()} onClick={() => void copyVisibleRows()}>Copy visible rows</Button>
          <Button type="button" variant="outline" disabled={exporting()} onClick={() => void exportCSV()}>Export CSV</Button>
          <Show when={downloadURL()}>{url => <a class="inline-flex h-10 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground" href={url()} download="query-result.csv">Download CSV</a>}</Show>
          <Show when={exportError()}><p role="alert" class="basis-full text-sm text-destructive">CSV export failed: {exportError()}</p></Show>
        </div>
        <div role="group" aria-label="Result sorting" class="result-sorting">
          <label class="result-sort-label">Sort by
            <select aria-label="Sort by" class="result-control" value={sortColumn() === undefined ? "" : String(sortColumn())} onChange={event => applySorting(event.currentTarget.value === "" ? undefined : Number(event.currentTarget.value), false)}>
              <option value="">Original query order</option>
              <For each={snapshot().columns}>{(column, index) => <option value={String(index())}>{column.name} (column {index() + 1})</option>}</For>
            </select>
          </label>
          <Show when={sortColumn() !== undefined}>
            <label class="result-sort-label">Direction
              <select aria-label="Sort direction" class="result-control" value={descending() ? "descending" : "ascending"} onChange={event => applySorting(sortColumn(), event.currentTarget.value === "descending")}>
                <option value="ascending">Ascending</option><option value="descending">Descending</option>
              </select>
            </label>
            <Button type="button" variant="outline" onClick={() => applySorting(undefined, false)}>Restore query order</Button>
          </Show>
        </div>
        </div>
        <p role="status" class="text-sm text-muted-foreground">
          <Show when={sortColumn() !== undefined} fallback="Original query order.">
            Sorted by {snapshot().columns[sortColumn() ?? 0]?.name} {descending() ? "descending" : "ascending"} across the cached snapshot. NULL values last.
          </Show>
        </p>
        <p class="text-xs text-muted-foreground">Sorting uses each column's data type across all cached rows. CSV exports the complete snapshot in original query order, including rows hidden by filters.</p>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div role="group" aria-label="Result view" class="inline-flex gap-1 rounded-lg border bg-card p-1">
            <Button type="button" size="sm" variant={view() === "table" ? "default" : "ghost"} aria-pressed={view() === "table"} onClick={() => setView("table")}>Table</Button>
            <Button type="button" size="sm" variant={view() === "text" ? "default" : "ghost"} aria-pressed={view() === "text"} onClick={() => setView("text")}>Text</Button>
          </div>
          <p class="text-xs text-muted-foreground">Copy includes this page only; formula-like text is escaped for spreadsheet paste.</p>
        </div>
        <Show when={copyMessage()}><p role="status" class="text-sm text-muted-foreground">{copyMessage()}</p></Show>
        <Show when={refreshing()}><p role="status" aria-busy="true" class="text-sm text-muted-foreground">Updating results… The rows below are from the previous view.</p></Show>
        <Show when={view() === "table"} fallback={<pre aria-label="Text results" class="result-text max-h-[36rem] overflow-auto rounded-lg border bg-card p-4 font-mono text-sm leading-6">{resultText(snapshot())}</pre>}>
        <div class="overflow-x-auto"><Table>
          <TableHeader><TableRow><For each={snapshot().columns}>{(column, index) => <TableHead aria-sort={sortColumn() === index() ? descending() ? "descending" : "ascending" : undefined}>
            <button class="text-left" title={sortColumn() !== index() ? "Sort ascending" : descending() ? "Restore query order" : "Sort descending"} onClick={() => cycleColumnSorting(index())}>
              {column.name} {sortColumn() === index() ? descending() ? "↓" : "↑" : "↕"}
              <span class="block text-xs text-muted-foreground">{LogicalType[column.logicalType]} · {column.dbTypeName}</span>
            </button>
          </TableHead>}</For></TableRow></TableHeader>
          <TableBody><For each={table.getRowModel().rows}>{row => <TableRow><For each={row.getVisibleCells()}>{cell => <TableCell>
            <button class="max-w-64 truncate rounded px-1 py-1 text-left font-mono text-sm hover:bg-muted focus-visible:bg-muted" onClick={() => setFullCell(String(cell.getValue()))} title="View full cell">{String(cell.getValue())}</button>
          </TableCell>}</For></TableRow>}</For></TableBody>
        </Table></div>
        </Show>
        <Show when={fullCell() !== undefined}><div><Button variant="ghost" onClick={() => setFullCell()}>Close cell</Button><pre aria-label="Full cell" class="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded border p-3">{fullCell()}</pre></div></Show>
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="text-sm">{snapshot().totalCount.toString()} rows · Page {snapshot().page} of {Math.max(snapshot().totalPages, 1)}</span>
          <label class="flex items-center gap-2 text-sm">Rows per page
          <select class="result-control w-auto" aria-label="Rows per page" value={pageSize()} onChange={event => { setPageSize(Number(event.currentTarget.value)); setPage(1); reload(); }}><For each={[10, 20, 50, 100]}>{size => <option value={size}>{size}</option>}</For></select></label>
          <div class="flex flex-wrap gap-2">
          <Button variant="outline" disabled={refreshing() || page() <= 1} onClick={() => { setPage(page() - 1); reload(); }}>Previous page</Button>
          <Button variant="outline" disabled={refreshing() || page() >= snapshot().totalPages} onClick={() => { setPage(page() + 1); reload(); }}>Next page</Button>
          </div>
        </div>
      </>}</Show>
  </section>;
};
