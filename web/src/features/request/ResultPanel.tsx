import { A } from "@solidjs/router";
import type { Component } from "solid-js";
import { For, Show, createEffect, createMemo, createSignal, on, onCleanup } from "solid-js";
import { createSolidTable, getCoreRowModel } from "@tanstack/solid-table";

import type { CellValue, QueryExecution, QueryResultPage, QueryResultRow } from "@/gen/portcullis/v1/query_executions_pb";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { LogicalType } from "@/gen/portcullis/v1/query_executions_pb";
import { errorMessage } from "@/entities/request/store";
import { executionsClient } from "@/shared/api/client";
import { createOpenFetch } from "@/shared/lib/openFetch";
import { Button } from "@/shared/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/table";

/** Formats one wire cell without coercing exact integers or decimals to JavaScript numbers. */
export function cellText(cell?: CellValue): string {
  if (!cell || cell.kind.case === "isNull" || cell.kind.case === undefined) return "NULL";
  if (cell.kind.case === "bytesValue") return "\\x" + Array.from(cell.kind.value, b => b.toString(16).padStart(2, "0")).join("");
  return String(cell.kind.value);
}

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
  const [exporting, setExporting] = createSignal(false);
  const [downloadURL, setDownloadURL] = createSignal<string>();
  const discardDownload = () => {
    const url = downloadURL();
    if (url) URL.revokeObjectURL(url);
    setDownloadURL();
  };
  onCleanup(discardDownload);
  let revision = 0;
  const read = createOpenFetch(async () => {
    const info = await executionsClient.get({ requestId: props.requestId ?? "" });
    const snapshot = info.resultAvailable ? await executionsClient.getResult({
      requestId: props.requestId ?? "", page: page(), pageSize: pageSize(), sortColumn: sortColumn(), descending: descending(), filter: filter(),
    }) : undefined;
    return { info, snapshot };
  }, ({ info, snapshot }) => { setExecution(info); setResult(snapshot); }, errorMessage);
  const id = createMemo(() => props.requestId);
  createEffect(on(id, (next) => {
    revision++;
    discardDownload();
    read.handleOpenChange(false);
    setExecution(); setResult(); setFullCell(); setExporting(false);
    setPage(1); setPageSize(20); setSortColumn(); setDescending(false); setFilter(""); setFilterDraft("");
    if (next !== undefined) read.handleOpenChange(true);
  }));
  const reload = () => { revision++; setResult(); setFullCell(); read.handleOpenChange(true); };
  onCleanup(() => { revision++; read.handleOpenChange(false); });
  const table = createSolidTable<QueryResultRow>({
    get data() { return result()?.rows ?? []; },
    get columns() { return (result()?.columns ?? []).map((c, i) => ({ id: String(i), accessorFn: (row: QueryResultRow) => cellText(row.cells[i]), header: c.name })); },
    getCoreRowModel: getCoreRowModel(), manualPagination: true, manualSorting: true, manualFiltering: true,
  });
  const exportCSV = async () => {
    const captured = revision;
    const same = read.captureSession();
    setExporting(true); read.setError("");
    discardDownload();
    try {
      const chunks: Uint8Array<ArrayBuffer>[] = [];
      for await (const chunk of executionsClient.exportCSV({ requestId: props.requestId ?? "" })) {
        if (!same() || captured !== revision) return;
        chunks.push(new Uint8Array(chunk.data));
      }
      if (!same() || captured !== revision) return;
      setDownloadURL(URL.createObjectURL(new Blob(chunks, { type: "text/csv;charset=utf-8" })));
    } catch (err) { if (same() && captured === revision) read.setError(errorMessage(err)); }
    finally { if (same() && captured === revision) setExporting(false); }
  };
  return <section aria-label="Query results" class="flex min-w-0 flex-col gap-6">
      <header><A href={`/requests/${props.requestId}`} class="text-sm underline">Back to request</A>
      <h1 class="mt-3 text-2xl font-semibold">Query result</h1><p class="text-sm text-muted-foreground">
        <Show when={execution()}>{e => <span>{e().rowsAffected.toString()} rows affected · {e().durationMs.toString()} ms</span>}</Show>
      </p></header>
      <Show when={read.loading()}><p role="status">Loading result…</p></Show>
      <Show when={read.error()}><p role="alert" class="text-destructive">{read.error()} Results may have expired or been evicted.</p></Show>
      <Show when={execution()?.state === AccessRequestState.OUTCOME_UNKNOWN}><p role="alert">Outcome unknown. Check the target database and audit history before creating another request. This execution will not retry.</p></Show>
      <Show when={execution()?.state === AccessRequestState.FAILED}><p role="alert">Execution failed. Submit a new request to try again.</p></Show>
      <Show when={execution()?.state === AccessRequestState.SUCCEEDED && !execution()?.resultAvailable}><p>Execution succeeded. The cached result is no longer available.</p></Show>
      <Show when={result()}>{snapshot => <>
        <Show when={snapshot().truncated}><p role="status">Result truncated by the row or byte limit. CSV contains only this cached snapshot.</p></Show>
        <p class="text-xs text-muted-foreground">Results expire after 15 minutes and may be evicted earlier.</p>
        <form class="flex flex-wrap gap-2" onSubmit={e => { e.preventDefault(); setFilter(filterDraft()); setPage(1); reload(); }}>
          <input aria-label="Filter results" class="rounded-md border px-3 text-sm" value={filterDraft()} maxLength={1000} onInput={e => setFilterDraft(e.currentTarget.value)} placeholder="Contains text in any column" />
          <Button variant="outline" type="submit">Filter results</Button>
          <Button type="button" variant="outline" disabled={exporting()} onClick={() => void exportCSV()}>Export CSV</Button>
          <Show when={downloadURL()}>{url => <a class="inline-flex items-center rounded-md border px-4 text-sm" href={url()} download="query-result.csv">Download CSV</a>}</Show>
        </form>
        <div class="overflow-x-auto"><Table>
          <TableHeader><TableRow><For each={snapshot().columns}>{(column, index) => <TableHead>
            <button class="text-left" onClick={() => { setDescending(sortColumn() === index() ? !descending() : false); setSortColumn(index()); setPage(1); reload(); }}>
              {column.name} {sortColumn() === index() ? descending() ? "↓" : "↑" : ""}
              <span class="block text-xs text-muted-foreground">{LogicalType[column.logicalType]} · {column.dbTypeName}</span>
            </button>
          </TableHead>}</For></TableRow></TableHeader>
          <TableBody><For each={table.getRowModel().rows}>{row => <TableRow><For each={row.getVisibleCells()}>{cell => <TableCell>
            <button class="max-w-64 truncate text-left font-mono text-sm" onClick={() => setFullCell(String(cell.getValue()))} title="View full cell">{String(cell.getValue())}</button>
          </TableCell>}</For></TableRow>}</For></TableBody>
        </Table></div>
        <Show when={fullCell() !== undefined}><div><Button variant="ghost" onClick={() => setFullCell()}>Close cell</Button><pre aria-label="Full cell" class="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded border p-3">{fullCell()}</pre></div></Show>
        <div class="flex items-center justify-between gap-2">
          <span class="text-sm">{snapshot().totalCount.toString()} rows · Page {snapshot().page} of {Math.max(snapshot().totalPages, 1)}</span>
          <select aria-label="Rows per page" value={pageSize()} onChange={e => { setPageSize(Number(e.currentTarget.value)); setPage(1); reload(); }}><For each={[10, 20, 50, 100]}>{size => <option value={size}>{size}</option>}</For></select>
          <Button variant="outline" disabled={page() <= 1} onClick={() => { setPage(page() - 1); reload(); }}>Previous page</Button>
          <Button variant="outline" disabled={page() >= snapshot().totalPages} onClick={() => { setPage(page() + 1); reload(); }}>Next page</Button>
        </div>
      </>}</Show>
  </section>;
};
