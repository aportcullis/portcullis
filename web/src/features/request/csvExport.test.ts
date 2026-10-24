import { describe, expect, it, vi } from "vitest";

import { createCsvExport, isCsvDownloadReady, CSV_CLIPBOARD_BYTE_LIMIT, CSV_DOWNLOAD_BYTE_LIMIT } from "@/features/request/csvExport";
import type { CsvExportController, CsvExportOptions, CsvExportRequest, CsvReadyDownload } from "@/features/request/csvExport";

/** Yields bounded byte fragments as one CSV response. */
async function* csvChunks(...chunks: Uint8Array[]): AsyncIterable<Uint8Array> {
  for (const chunk of chunks) yield chunk;
}

/** Exposes a held next chunk without timers or network scheduling. */
function deferredChunk() {
  let resolve = (_chunk: Uint8Array): void => { throw new Error("Missing resolver"); };
  const promise = new Promise<Uint8Array>(settle => { resolve = settle; });
  return { promise, resolve };
}

const defaultCsvStream: CsvExportOptions["streamCSV"] = () => csvChunks(
  new TextEncoder().encode("name,value\n'=formula,9007199254740993\n"),
);

/** Requires a recorded export before inspecting its request and cancellation signal. */
function requireFirstExportCall(
  calls: ReadonlyArray<Parameters<CsvExportOptions["streamCSV"]>>,
): { request: CsvExportRequest; signal: AbortSignal } {
  for (const [request, signal] of calls) {
    if (!(signal instanceof AbortSignal)) throw new Error("Expected an export cancellation signal.");
    return { request, signal };
  }
  throw new Error("Expected an authorized CSV export call.");
}

/** Requires a recorded file preparation instead of inventing empty CSV bytes. */
function requireFirstDownloadChunks(
  calls: ReadonlyArray<Parameters<CsvExportOptions["createDownload"]>>,
): Uint8Array<ArrayBuffer>[] {
  for (const [chunks] of calls) return chunks;
  throw new Error("Expected prepared CSV download chunks.");
}

/** Requires a real first fragment before decoding fixture output. */
function requireFirstCsvChunk(chunks: Uint8Array<ArrayBuffer>[]): Uint8Array<ArrayBuffer> {
  for (const chunk of chunks) return chunk;
  throw new Error("Expected a nonempty CSV chunk list.");
}

/** Narrows completed download state before asserting its artifact URL. */
function requireReadyDownload(controller: CsvExportController): CsvReadyDownload {
  const { download } = controller.snapshot();
  if (!isCsvDownloadReady(download)) throw new Error("Expected a complete CSV download.");
  return download;
}

/** Records output ownership separately from stream preparation. */
function exportHarness(streamCSV: CsvExportOptions["streamCSV"] = defaultCsvStream) {
  const writeClipboard = vi.fn(async (_text: string) => {});
  const createDownload = vi.fn((_chunks: Uint8Array<ArrayBuffer>[]) => "blob:csv-download");
  const revokeDownload = vi.fn();
  const onRefused = vi.fn();
  const onUnavailable = vi.fn();
  const reader = vi.fn(streamCSV);
  const controller = createCsvExport({
    streamCSV: reader,
    writeClipboard,
    createDownload,
    revokeDownload,
    formatError: error => error instanceof Error ? error.message : "Export failed.",
    isAuthorizationRefusal: error => error === "refused",
    isUnavailable: error => error === "expired",
    onRefused,
    onUnavailable,
    onChange: () => {},
  });
  return { controller, reader, writeClipboard, createDownload, revokeDownload, onRefused, onUnavailable };
}

describe("CSV dialog export choices", () => {
  it("opens, configures and closes without preparing CSV", () => {
    const { controller, reader } = exportHarness();
    controller.open("request-1");
    expect(controller.snapshot().open).toBe(true);
    expect(controller.snapshot().spreadsheetSafe).toBe(true);
    controller.setSafety(false);
    controller.acknowledgeRawRisk(true);
    controller.close();
    expect(reader).not.toHaveBeenCalled();
    expect(controller.snapshot().open).toBe(false);
  });

  it("copies exact authorized bytes across UTF-8 chunk boundaries", async () => {
    const csv = "label,value\n한글,9007199254740993\n";
    const bytes = new TextEncoder().encode(csv);
    const harness = exportHarness(() => csvChunks(bytes.slice(0, 14), bytes.slice(14, 15), bytes.slice(15)));
    harness.controller.open("request-1");
    await harness.controller.copyCSV();
    expect(requireFirstExportCall(harness.reader.mock.calls).request).toEqual({ requestId: "request-1", raw: false });
    expect(harness.writeClipboard).toHaveBeenCalledWith(csv);
    expect(harness.controller.snapshot().message).toBe("Copied CSV to clipboard.");
    expect(harness.controller.snapshot().retainedBytes).toBe(0);
  });

  it("prepares default-safe CSV for native saving and revokes it when closed", async () => {
    const harness = exportHarness();
    harness.controller.open("request-1");
    await harness.controller.downloadCSV();
    expect(requireFirstExportCall(harness.reader.mock.calls).request).toEqual({ requestId: "request-1", raw: false });
    const chunks = requireFirstDownloadChunks(harness.createDownload.mock.calls);
    expect(new TextDecoder().decode(requireFirstCsvChunk(chunks))).toContain("'=formula,9007199254740993");
    expect(requireReadyDownload(harness.controller).url).toBe("blob:csv-download");
    harness.controller.close();
    expect(harness.revokeDownload).toHaveBeenCalledWith("blob:csv-download");
    expect(harness.controller.snapshot().download).toEqual({ kind: "absent" });
  });

  it("requires explicit raw acknowledgment before either destination can export raw CSV", async () => {
    const harness = exportHarness();
    harness.controller.open("request-1");
    harness.controller.setSafety(false);
    await harness.controller.copyCSV();
    await harness.controller.downloadCSV();
    expect(harness.reader).not.toHaveBeenCalled();
    expect(harness.controller.snapshot().error).toContain("raw CSV risk");
    harness.controller.acknowledgeRawRisk(true);
    await harness.controller.downloadCSV();
    expect(requireFirstExportCall(harness.reader.mock.calls).request).toEqual({ requestId: "request-1", raw: true });
    harness.controller.setSafety(true);
    expect(harness.controller.snapshot().download).toEqual({ kind: "absent" });
    expect(harness.revokeDownload).toHaveBeenCalledWith("blob:csv-download");
  });

  it("coalesces tiny chunks and ignores empty chunks instead of retaining an unbounded object list", async () => {
    const harness = exportHarness(() => (async function* () {
      for (let index = 0; index < 10000; index++) {
        yield new Uint8Array();
        yield new Uint8Array([65]);
      }
    })());
    harness.controller.open("request-1");
    await harness.controller.downloadCSV();
    const chunks = requireFirstDownloadChunks(harness.createDownload.mock.calls);
    expect(chunks).toHaveLength(1);
    expect(requireFirstCsvChunk(chunks).byteLength).toBe(10000);
    expect(harness.controller.snapshot().retainedBytes).toBe(10000);
  });
});

describe("CSV preparation bounds and refused output", () => {
  it("rechecks dialog ownership before invoking clipboard output after a progress callback closes it", async () => {
    const writeClipboard = vi.fn(async (_text: string) => {});
    const controller: CsvExportController = createCsvExport({
      streamCSV: () => csvChunks(new TextEncoder().encode("secret\n")),
      writeClipboard,
      createDownload: () => "blob:csv",
      revokeDownload: () => {},
      formatError: () => "Export failed.",
      isAuthorizationRefusal: () => false,
      isUnavailable: () => false,
      onRefused: () => {},
      onUnavailable: () => {},
      onChange: () => {
        if (controller.snapshot().phase === "copying") controller.close();
      },
    });
    controller.open("request-1");
    await controller.copyCSV();
    expect(writeClipboard).not.toHaveBeenCalled();
    expect(controller.snapshot().open).toBe(false);
  });

  it("refuses oversized clipboard output before decoding or constructing its full string", async () => {
    const decode = vi.spyOn(TextDecoder.prototype, "decode");
    const harness = exportHarness((_request, _requestSignal) => {
      return csvChunks(new Uint8Array(CSV_CLIPBOARD_BYTE_LIMIT), new Uint8Array([10]));
    });
    harness.controller.open("request-1");
    await harness.controller.copyCSV();
    expect(decode).not.toHaveBeenCalled();
    decode.mockRestore();
    expect(requireFirstExportCall(harness.reader.mock.calls).signal.aborted).toBe(true);
    expect(harness.writeClipboard).not.toHaveBeenCalled();
    expect(harness.controller.snapshot().clipboardTooLarge).toBe(true);
    expect(harness.controller.snapshot().retainedBytes).toBe(0);
    await harness.controller.downloadCSV();
    expect(requireReadyDownload(harness.controller).url).toBe("blob:csv-download");
  });

  it("keeps a download fallback after clipboard denial without silently copying again", async () => {
    const harness = exportHarness();
    harness.writeClipboard.mockRejectedValueOnce(new Error("Clipboard denied"));
    harness.controller.open("request-1");
    await harness.controller.copyCSV();
    expect(harness.controller.snapshot().error).toBe("Clipboard access was denied or unavailable. Download the file instead.");
    expect(harness.controller.snapshot().open).toBe(true);
    expect(requireReadyDownload(harness.controller).url).toBe("blob:csv-download");
    await harness.controller.downloadCSV();
    expect(harness.controller.snapshot().error).toBe("");
    expect(harness.reader).toHaveBeenCalledTimes(1);
    expect(harness.writeClipboard).toHaveBeenCalledTimes(1);
  });

  it("aborts file preparation at its hard byte limit and publishes no partial file", async () => {
    const harness = exportHarness((_request, _requestSignal) => {
      return csvChunks(new Uint8Array(CSV_DOWNLOAD_BYTE_LIMIT), new Uint8Array([10]));
    });
    harness.controller.open("request-1");
    await harness.controller.downloadCSV();
    expect(requireFirstExportCall(harness.reader.mock.calls).signal.aborted).toBe(true);
    expect(harness.createDownload).not.toHaveBeenCalled();
    expect(harness.controller.snapshot().retainedBytes).toBe(0);
    expect(harness.controller.snapshot().error).toContain("128 MiB");
  });

  it("cancels preparation, releases retained chunks and ignores a late chunk", async () => {
    const held = deferredChunk();
    const harness = exportHarness((_request, _requestSignal) => {
      return (async function* () { yield new Uint8Array([65]); yield await held.promise; })();
    });
    harness.controller.open("request-1");
    const preparing = harness.controller.downloadCSV();
    await vi.waitFor(() => expect(harness.controller.snapshot().byteCount).toBe(1));
    harness.controller.cancel();
    expect(requireFirstExportCall(harness.reader.mock.calls).signal.aborted).toBe(true);
    expect(harness.controller.snapshot().retainedBytes).toBe(0);
    expect(harness.controller.snapshot().busy).toBe(false);
    held.resolve(new Uint8Array([66]));
    await preparing;
    expect(harness.createDownload).not.toHaveBeenCalled();
    expect(harness.controller.snapshot().message).toBe("CSV export cancelled.");
  });

  it("fences a closing and reopening dialog from the previous preparation", async () => {
    const held = deferredChunk();
    const harness = exportHarness(() => (async function* () { yield await held.promise; })());
    harness.controller.open("request-old");
    const preparing = harness.controller.copyCSV();
    harness.controller.close();
    harness.controller.open("request-new");
    held.resolve(new TextEncoder().encode("old-secret\n"));
    await preparing;
    expect(harness.writeClipboard).not.toHaveBeenCalled();
    expect(harness.createDownload).not.toHaveBeenCalled();
    expect(harness.controller.snapshot().phase).toBe("idle");
    expect(harness.controller.snapshot().byteCount).toBe(0);
  });

  it("closes and discards protected output when the server refuses authorization", async () => {
    const harness = exportHarness(() => (async function* () { throw "refused"; yield new Uint8Array(); })());
    harness.controller.open("request-1");
    await harness.controller.downloadCSV();
    expect(harness.onRefused).toHaveBeenCalledTimes(1);
    expect(harness.controller.snapshot().open).toBe(false);
    expect(harness.controller.snapshot().retainedBytes).toBe(0);
    expect(harness.createDownload).not.toHaveBeenCalled();
  });

  it("reports failed streams in the open dialog and permits an explicit new attempt", async () => {
    let failing = true;
    const harness = exportHarness(() => (async function* () {
      yield new Uint8Array([65]);
      if (failing) throw new Error("CSV service unavailable");
    })());
    harness.controller.open("request-1");
    await harness.controller.downloadCSV();
    expect(harness.controller.snapshot().error).toBe("CSV export failed: CSV service unavailable");
    expect(harness.controller.snapshot().download).toEqual({ kind: "absent" });
    failing = false;
    await harness.controller.downloadCSV();
    expect(requireReadyDownload(harness.controller).url).toBe("blob:csv-download");
  });
});
