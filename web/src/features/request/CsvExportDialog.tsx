import { Code, ConnectError } from "@connectrpc/connect";
import type { Component, JSX } from "solid-js";
import { Show, createEffect, createMemo, createSignal, createUniqueId, on, onCleanup, splitProps } from "solid-js";

import { createCsvExport, isCsvDownloadReady } from "@/features/request/csvExport";
import type { CsvExportSnapshot, CsvReadyDownload } from "@/features/request/csvExport";
import { describeResultError } from "@/features/request/resultErrors";
import { executionsClient } from "@/shared/api/client";
import { errorMessage, isAuthorizationRefusal } from "@/shared/api/errors";
import { Button, buttonVariants } from "@/shared/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/shared/ui/dialog";

/** Defines the protected snapshot and refusal callbacks owned by the result page. */
export type CsvExportDialogProps = {
  requestId: string;
  onRefused: () => void;
  onUnavailable: (error: unknown) => void;
};

/** Configures bounded CSV clipboard or file output inside one cancellable dialog. */
export const CsvExportDialog: Component<CsvExportDialogProps> = (props) => {
  const [dialogProps] = splitProps(props, ["requestId", "onRefused", "onUnavailable"]);
  const [revision, setRevision] = createSignal(0);
  const saveLinkID: string = createUniqueId();
  let dialogGeneration = 0;
  const controller = createCsvExport({
    streamCSV: async function* (request, signal) {
      for await (const chunk of executionsClient.exportCSV(request, { signal })) yield chunk.data;
    },
    writeClipboard: async text => {
      const browserClipboard: unknown = navigator.clipboard;
      assertCsvClipboardWriter(browserClipboard);
      await browserClipboard.writeText(text);
    },
    createDownload: chunks => URL.createObjectURL(new Blob(chunks, { type: "text/csv;charset=utf-8" })),
    revokeDownload: url => URL.revokeObjectURL(url),
    formatError: error => describeResultError(error, errorMessage),
    isAuthorizationRefusal,
    isUnavailable: error => error instanceof ConnectError &&
      [Code.FailedPrecondition, Code.NotFound].includes(error.code),
    onRefused: () => dialogProps.onRefused(),
    onUnavailable: error => dialogProps.onUnavailable(error),
    onChange: () => setRevision(current => current + 1),
  });
  const state = createMemo<CsvExportSnapshot>(() => {
    revision();
    return controller.snapshot();
  });
  const readyDownload = createMemo<CsvDownloadPresentation>(() => {
    const { download } = state();
    return isCsvDownloadReady(download) ? download : false;
  });
  const isCsvExportActionDisabled = (): boolean => {
    const { busy, unavailable, spreadsheetSafe, rawAcknowledged } = state();
    return busy || unavailable || !spreadsheetSafe && !rawAcknowledged;
  };
  const changeOpen = (open: boolean): void => {
    dialogGeneration++;
    if (open) controller.open(dialogProps.requestId);
    else controller.close();
  };
  const prepareCsvDownload = async (): Promise<void> => {
    const capturedGeneration: number = dialogGeneration;
    const originalFocus: unknown = document.activeElement;
    const restorePrimaryFocus: boolean = isCsvPreparationButton(originalFocus);
    await controller.downloadCSV();
    if (!restorePrimaryFocus) return;
    queueMicrotask(() => {
      const { open, download } = state();
      if (capturedGeneration !== dialogGeneration || !open || !isCsvDownloadReady(download)) return;
      const currentFocus: unknown = document.activeElement;
      if (currentFocus !== originalFocus && currentFocus !== document.body) return;
      const saveLink: unknown = document.getElementById(saveLinkID);
      if (isCsvSaveAnchor(saveLink)) saveLink.focus();
    });
  };
  const changeSpreadsheetSafety: JSX.EventHandler<HTMLInputElement, Event> = event => {
    const { currentTarget } = event;
    assertCsvCheckboxInput(currentTarget);
    controller.setSafety(currentTarget.checked);
  };
  const changeRawAcknowledgment: JSX.EventHandler<HTMLInputElement, Event> = event => {
    const { currentTarget } = event;
    assertCsvCheckboxInput(currentTarget);
    controller.acknowledgeRawRisk(currentTarget.checked);
  };
  createEffect(on(() => dialogProps.requestId, () => {
    dialogGeneration++;
    controller.close();
  }));
  onCleanup(() => {
    dialogGeneration++;
    controller.dispose();
  });

  return (
    <Dialog open={state().open} onOpenChange={changeOpen}>
      <DialogTrigger as={Button} type="button" size="sm" variant="outline">
        Export CSV
      </DialogTrigger>
      <DialogContent aria-modal="true" class="w-[calc(100%_-_2rem)] max-w-lg gap-4 p-4 sm:p-6">
        <DialogHeader>
          <DialogTitle class="pr-8">Export CSV</DialogTitle>
          <DialogDescription>
            Whole cached snapshot in original query order, regardless of the visible filter or sort.
          </DialogDescription>
        </DialogHeader>
        <p class="text-sm">
          Includes column headers and only the cached rows retained by the execution limits. Exporting does not run SQL again.
        </p>
        <label class="flex items-start gap-3 rounded-lg border p-3 text-sm">
          <input
            type="checkbox"
            class="size-6 shrink-0 accent-primary"
            checked={state().spreadsheetSafe}
            disabled={state().busy}
            onChange={changeSpreadsheetSafety}
          />
          <span>Spreadsheet-safe output</span>
        </label>
        <Show when={state().spreadsheetSafe}>
          <p class="text-sm text-muted-foreground">
            Formula-like headers and values are escaped for spreadsheet use.
          </p>
        </Show>
        <Show when={!state().spreadsheetSafe}>
          <p class="text-sm text-destructive-text">
            Raw CSV can run formulas when opened in spreadsheet software.
          </p>
          <label class="flex items-start gap-3 rounded-lg border p-3 text-sm">
            <input
              type="checkbox"
              class="size-6 shrink-0 accent-primary"
              checked={state().rawAcknowledged}
              disabled={state().busy}
              onChange={changeRawAcknowledgment}
            />
            <span>I understand the raw CSV risk</span>
          </label>
        </Show>
        <p class="text-xs text-muted-foreground">
          Clipboard copy is limited to 2 MiB. Larger CSV files remain available through download.
        </p>
        <Show when={state().busy}>
          <p role="status" aria-live="polite" aria-busy="true" class="text-sm">
            {state().phase === "copying" ? "Copying CSV to clipboard…" : `Preparing CSV: ${state().byteCount} bytes`}
          </p>
        </Show>
        <Show when={state().error}>
          <p role="alert" class="text-sm text-destructive-text">{state().error}</p>
        </Show>
        <Show when={state().message}>
          <p role="status" class="text-sm">{state().message}</p>
        </Show>
        <DialogFooter class="gap-2 sm:space-x-0">
          <Button type="button" size="sm" variant="ghost" onClick={() => changeOpen(false)}>
            Close export
          </Button>
          <Show when={state().busy}>
            <Button type="button" size="sm" variant="outline" onClick={() => controller.cancel()}>
              Cancel export
            </Button>
          </Show>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={isCsvExportActionDisabled() || state().clipboardTooLarge}
            onClick={() => void controller.copyCSV()}
          >
            Copy CSV
          </Button>
          <Show
            when={readyDownload()}
            fallback={
              <Button
                type="button"
                size="sm"
                data-csv-download-prepare="true"
                disabled={isCsvExportActionDisabled()}
                onClick={() => void prepareCsvDownload()}
              >
                Download CSV
              </Button>
            }
          >
            {download => (
              <a
                id={saveLinkID}
                class={buttonVariants({ size: "sm" })}
                href={download().url}
                download="query-result.csv"
                onClick={() => void controller.downloadCSV()}
              >
                Save CSV file
              </a>
            )}
          </Show>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

type CsvDownloadPresentation = CsvReadyDownload | false;
type CsvClipboardWriter = { writeText: (text: string) => Promise<void> };

/** Recognizes the browser capability needed for the CSV clipboard destination. */
function isCsvClipboardWriter(value: unknown): value is CsvClipboardWriter {
  return value instanceof Object && "writeText" in value && typeof value.writeText === "function";
}

/** Refuses unsupported clipboard APIs before invoking an unverified browser method. */
function assertCsvClipboardWriter(value: unknown): asserts value is CsvClipboardWriter {
  if (!isCsvClipboardWriter(value)) throw new Error("Clipboard unavailable");
}

/** Recognizes the native checkbox boundary used by CSV safety choices. */
function isCsvCheckboxInput(value: unknown): value is HTMLInputElement {
  return value instanceof HTMLInputElement && value.type === "checkbox";
}

/** Checks checkbox events before reading their safety-selection state. */
function assertCsvCheckboxInput(value: unknown): asserts value is HTMLInputElement {
  if (!isCsvCheckboxInput(value)) throw new Error("CSV safety control is not a checkbox.");
}

/** Recognizes the preparation button before preserving its primary-action focus. */
function isCsvPreparationButton(value: unknown): value is HTMLButtonElement {
  return value instanceof HTMLButtonElement && value.getAttribute("data-csv-download-prepare") === "true";
}

/** Recognizes a mounted native download target before moving keyboard focus. */
function isCsvSaveAnchor(value: unknown): value is HTMLAnchorElement {
  return value instanceof HTMLAnchorElement && value.hasAttribute("download");
}
