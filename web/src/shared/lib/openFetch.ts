import { createSignal } from "solid-js";

import { createDialogSession } from "@/shared/lib/dialogSession";

/** Fetches data when a dialog opens and ignores responses from superseded sessions. */
export function createOpenFetch<T>(
  fetcher: () => Promise<T>,
  onLoaded: (value: T) => void,
  formatError: (err: unknown) => string,
) {
  const [open, setOpen] = createSignal(false);
  const [loading, setLoading] = createSignal(false);
  const [error, setError] = createSignal("");
  const { discardSession, captureSession, runInSession } = createDialogSession();

  const refetch = () => {
    discardSession();
    const isSameSession = captureSession();
    setError("");
    setLoading(true);
    void fetcher()
      .then((value) => {
        if (isSameSession()) onLoaded(value);
      })
      .catch((err: unknown) => {
        if (isSameSession()) setError(formatError(err));
      })
      .finally(() => {
        if (isSameSession()) setLoading(false);
      });
  };

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (next) {
      refetch();
    } else {
      discardSession(); // fence in-flight responses from the closed dialog
    }
  };

  return { open, loading, error, setError, captureSession, runInSession, handleOpenChange };
}
