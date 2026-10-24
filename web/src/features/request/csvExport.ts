/** Limits complete CSV clipboard text before its string is constructed. */
export const CSV_CLIPBOARD_BYTE_LIMIT = 2 * 1024 * 1024;
/** Limits client-side CSV preparation independently of clipboard access. */
export const CSV_DOWNLOAD_BYTE_LIMIT = 128 * 1024 * 1024;

const csvBufferBlockSize = 64 * 1024;

/** Describes one authorized snapshot export with an explicit safety choice. */
export type CsvExportRequest = {
  requestId: string;
  raw: boolean;
};
/** Identifies the current export interaction without exposing temporary bytes. */
export type CsvExportPhase = "closed" | "idle" | "preparing" | "copying" | "ready" | "cancelled" | "error";
/** Defines the I/O ports owned by the CSV interaction. */
export type CsvExportOptions = {
  streamCSV: (request: CsvExportRequest, signal: AbortSignal) => AsyncIterable<Uint8Array>;
  writeClipboard: (text: string) => Promise<void>;
  createDownload: (chunks: Uint8Array<ArrayBuffer>[]) => string;
  revokeDownload: (url: string) => void;
  formatError: (error: unknown) => string;
  isAuthorizationRefusal: (error: unknown) => boolean;
  isUnavailable: (error: unknown) => boolean;
  onRefused: () => void;
  onUnavailable: (error: unknown) => void;
  onChange: () => void;
};
/** Reports dialog state and bounded preparation progress. */
export type CsvExportSnapshot = {
  open: boolean;
  phase: CsvExportPhase;
  spreadsheetSafe: boolean;
  rawAcknowledged: boolean;
  byteCount: number;
  retainedBytes: number;
  busy: boolean;
  error: string;
  message: string;
  clipboardTooLarge: boolean;
  unavailable: boolean;
  download: CsvDownloadState;
};
/** Distinguishes absent output from a complete downloadable CSV artifact. */
export type CsvDownloadState = { kind: "absent" } | CsvReadyDownload;
/** Describes a completed CSV file whose URL must be released on dismissal. */
export type CsvReadyDownload = {
  kind: "ready";
  url: string;
  byteCount: number;
};
/** Owns one dialog's authorized export, cancellation and temporary output. */
export type CsvExportController = {
  open: (requestId: string) => void;
  close: () => void;
  setSafety: (safe: boolean) => void;
  acknowledgeRawRisk: (acknowledged: boolean) => void;
  copyCSV: () => Promise<void>;
  downloadCSV: () => Promise<void>;
  cancel: () => void;
  dispose: () => void;
  snapshot: () => CsvExportSnapshot;
};

/** Creates a CSV export interaction from consumer-owned streaming and output ports. */
export function createCsvExport(options: CsvExportOptions): CsvExportController {
  const {
    streamCSV,
    writeClipboard,
    createDownload,
    revokeDownload,
    formatError,
    isAuthorizationRefusal,
    isUnavailable,
    onRefused,
    onUnavailable,
    onChange,
  } = options;
  let isOpen = false;
  let disposed = false;
  let generation = 0;
  let requestId = "";
  let phase: CsvExportPhase = "closed";
  let spreadsheetSafe = true;
  let rawAcknowledged = false;
  let byteCount = 0;
  let error = "";
  let message = "";
  let clipboardTooLarge = false;
  let unavailable = false;
  let download: CsvDownloadState = { kind: "absent" };
  let operationState: CsvOperationState = { kind: "idle" };

  const notifyChange = (): void => {
    if (!disposed) onChange();
  };
  const discardDownload = (): void => {
    if (isCsvDownloadReady(download)) {
      const { url } = download;
      revokeDownload(url);
    }
    download = { kind: "absent" };
  };
  const invalidatePreparation = (): void => {
    generation++;
    if (isCsvOperationActive(operationState)) {
      const { operation } = operationState;
      operation.controller.abort();
      operation.chunks.length = 0;
    }
    discardDownload();
    byteCount = 0;
    error = "";
    message = "";
    clipboardTooLarge = false;
    unavailable = false;
  };
  const close = (): void => {
    if (disposed) return;
    invalidatePreparation();
    isOpen = false;
    requestId = "";
    phase = "closed";
    notifyChange();
  };
  const isCurrentOperation = (operation: CsvExportOperation): boolean => !disposed && isOpen &&
    operation.generation === generation && isCsvOperationActive(operationState) &&
    operationState.operation === operation;
  const prepareDownload = (operation: CsvExportOperation): void => {
    discardDownload();
    download = {
      kind: "ready",
      url: createDownload(completedCSVChunks(operation)),
      byteCount: operation.byteCount,
    };
  };
  const prepareCSV = async (destination: CsvExportDestination): Promise<void> => {
    if (!isOpen || disposed || unavailable) return;
    if (!spreadsheetSafe && !rawAcknowledged) {
      error = "Acknowledge the raw CSV risk before exporting.";
      notifyChange();
      return;
    }
    if (isCsvOperationActive(operationState)) {
      if (phase !== "preparing" && phase !== "copying") {
        error = "The previous CSV export is still stopping. Try again shortly.";
        notifyChange();
      }
      return;
    }
    if (destination === "download" && isCsvDownloadReady(download)) {
      error = "";
      message = "CSV file is ready to save.";
      phase = "ready";
      notifyChange();
      return;
    }
    discardDownload();
    error = "";
    message = "";
    clipboardTooLarge = false;
    byteCount = 0;
    phase = "preparing";
    const operation: CsvExportOperation = {
      generation,
      controller: new AbortController(),
      chunks: [],
      byteCount: 0,
      lastChunkBytes: 0,
    };
    operationState = { kind: "active", operation };
    notifyChange();
    try {
      if (!isCurrentOperation(operation)) return;
      const request: CsvExportRequest = { requestId, raw: !spreadsheetSafe };
      for await (const chunk of streamCSV(request, operation.controller.signal)) {
        if (!isCurrentOperation(operation)) return;
        assertCsvBytes(chunk);
        if (chunk.byteLength === 0) continue;
        const nextByteCount = operation.byteCount + chunk.byteLength;
        if (nextByteCount > CSV_DOWNLOAD_BYTE_LIMIT) {
          operation.controller.abort();
          throw new CsvExportLimitError("CSV exceeds the 128 MiB file preparation limit.");
        }
        if (destination === "clipboard" && nextByteCount > CSV_CLIPBOARD_BYTE_LIMIT) {
          operation.controller.abort();
          clipboardTooLarge = true;
          throw new CsvExportLimitError("CSV exceeds the 2 MiB clipboard limit. Download the file instead.");
        }
        appendCSVBytes(operation, chunk);
        operation.byteCount = nextByteCount;
        byteCount = nextByteCount;
        notifyChange();
      }
      if (!isCurrentOperation(operation)) return;
      if (destination === "download") {
        prepareDownload(operation);
        message = "CSV file is ready to save.";
      } else {
        phase = "copying";
        notifyChange();
        if (!isCurrentOperation(operation)) return;
        try {
          await writeClipboard(decodeClipboardCSV(completedCSVChunks(operation), operation.byteCount));
          if (!isCurrentOperation(operation)) return;
          message = "Copied CSV to clipboard.";
        } catch {
          if (!isCurrentOperation(operation)) return;
          prepareDownload(operation);
          error = "Clipboard access was denied or unavailable. Download the file instead.";
        }
      }
      phase = error === "" ? "ready" : "error";
    } catch (failure) {
      if (!isCurrentOperation(operation)) return;
      discardDownload();
      if (isAuthorizationRefusal(failure)) {
        close();
        onRefused();
        return;
      }
      unavailable = isUnavailable(failure);
      error = failure instanceof CsvExportLimitError
        ? failure.message
        : `CSV export failed: ${formatError(failure)}`;
      phase = "error";
      if (unavailable) onUnavailable(failure);
    } finally {
      operation.chunks.length = 0;
      if (isCsvOperationActive(operationState) && operationState.operation === operation)
        operationState = { kind: "idle" };
      if (operation.generation === generation) notifyChange();
    }
  };

  return {
    open: nextRequestId => {
      if (disposed || isOpen && requestId === nextRequestId) return;
      invalidatePreparation();
      requestId = nextRequestId;
      isOpen = true;
      spreadsheetSafe = true;
      rawAcknowledged = false;
      phase = "idle";
      notifyChange();
    },
    close,
    setSafety: safe => {
      if (disposed || !isOpen || unavailable || spreadsheetSafe === safe) return;
      invalidatePreparation();
      spreadsheetSafe = safe;
      rawAcknowledged = false;
      phase = "idle";
      notifyChange();
    },
    acknowledgeRawRisk: acknowledged => {
      if (disposed || !isOpen || unavailable || spreadsheetSafe || rawAcknowledged === acknowledged) return;
      invalidatePreparation();
      rawAcknowledged = acknowledged;
      phase = "idle";
      notifyChange();
    },
    copyCSV: () => prepareCSV("clipboard"),
    downloadCSV: () => prepareCSV("download"),
    cancel: () => {
      if (disposed || !isOpen) return;
      invalidatePreparation();
      phase = "cancelled";
      message = "CSV export cancelled.";
      notifyChange();
    },
    dispose: () => {
      if (disposed) return;
      close();
      disposed = true;
    },
    snapshot: (): CsvExportSnapshot => ({
      open: isOpen,
      phase,
      spreadsheetSafe,
      rawAcknowledged,
      byteCount,
      retainedBytes: (isCsvDownloadReady(download) ? download.byteCount : 0) +
        (isCsvOperationActive(operationState)
          ? operationState.operation.chunks.reduce((total, chunk) => total + chunk.byteLength, 0)
          : 0),
      busy: phase === "preparing" || phase === "copying",
      error,
      message,
      clipboardTooLarge,
      unavailable,
      download,
    }),
  };
}

type CsvExportDestination = "clipboard" | "download";
type CsvOperationState = { kind: "idle" } | CsvActiveOperation;
type CsvActiveOperation = { kind: "active"; operation: CsvExportOperation };
type CsvExportOperation = {
  generation: number;
  controller: AbortController;
  chunks: Uint8Array<ArrayBuffer>[];
  byteCount: number;
  lastChunkBytes: number;
};

/** Identifies a bounded export refusal that can retain file-download availability. */
class CsvExportLimitError extends Error {}

/** Narrows complete file output before reading its URL and byte count. */
export function isCsvDownloadReady(download: CsvDownloadState): download is CsvReadyDownload {
  return download.kind === "ready";
}

/** Narrows an unsettled operation without inventing an absent operation value. */
function isCsvOperationActive(state: CsvOperationState): state is CsvActiveOperation {
  return state.kind === "active";
}

/** Recognizes binary CSV fragments at the streaming boundary. */
function isCsvBytes(value: unknown): value is Uint8Array {
  return value instanceof Uint8Array;
}

/** Refuses malformed transport fragments before their byte lengths are consumed. */
function assertCsvBytes(value: unknown): asserts value is Uint8Array {
  if (!isCsvBytes(value)) throw new Error("CSV stream returned an invalid byte fragment.");
}

/** Recognizes an owned browser buffer without accepting shared backing memory. */
function isOwnedCsvBuffer(value: unknown): value is Uint8Array<ArrayBuffer> {
  return isCsvBytes(value) && value.buffer instanceof ArrayBuffer;
}

/** Requires a valid owned buffer at a checked internal block index. */
function requireCsvBuffer(chunks: Uint8Array<ArrayBuffer>[], index: number): Uint8Array<ArrayBuffer> {
  if (!Number.isSafeInteger(index) || index < 0 || index >= chunks.length)
    throw new Error("CSV buffer index is outside its retained range.");
  const buffer: unknown = chunks[index];
  if (!isOwnedCsvBuffer(buffer))
    throw new Error("CSV buffer does not own its backing memory.");
  return buffer;
}

/** Coalesces arbitrary transport fragments into a bounded number of fixed blocks. */
function appendCSVBytes(operation: CsvExportOperation, chunk: Uint8Array): void {
  let offset = 0;
  while (offset < chunk.byteLength) {
    if (operation.chunks.length === 0 || operation.lastChunkBytes === csvBufferBlockSize) {
      operation.chunks.push(new Uint8Array(csvBufferBlockSize));
      operation.lastChunkBytes = 0;
    }
    const tail: Uint8Array<ArrayBuffer> = requireCsvBuffer(operation.chunks, operation.chunks.length - 1);
    const copiedBytes = Math.min(tail.byteLength - operation.lastChunkBytes, chunk.byteLength - offset);
    tail.set(chunk.subarray(offset, offset + copiedBytes), operation.lastChunkBytes);
    operation.lastChunkBytes += copiedBytes;
    offset += copiedBytes;
  }
}

/** Exposes only initialized CSV bytes to clipboard and download destinations. */
function completedCSVChunks(operation: CsvExportOperation): Uint8Array<ArrayBuffer>[] {
  return operation.chunks.map((chunk, index) => index === operation.chunks.length - 1
    ? chunk.subarray(0, operation.lastChunkBytes)
    : chunk);
}

/** Constructs clipboard text only after all streamed bytes fit its smaller limit. */
function decodeClipboardCSV(chunks: Uint8Array<ArrayBuffer>[], byteCount: number): string {
  if (byteCount > CSV_CLIPBOARD_BYTE_LIMIT) throw new CsvExportLimitError("CSV exceeds the clipboard limit.");
  const bytes = new Uint8Array(byteCount);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
}
