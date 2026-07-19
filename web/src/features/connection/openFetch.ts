import { createSignal } from "solid-js";

import { errorMessage } from "@/entities/connection/store";

// createOpenFetch owns the fetch-on-open lifecycle every read-on-open dialog
// needs: run the fetcher when the dialog opens and fence EVERY callback with a
// sequence, so a slow response from an earlier open (or from a dialog that has
// since closed) can never overwrite a later open's state. Extracted because
// three dialogs hand-copied the guard and a fix applied to two of the copies
// would leave the third overwriting newer state (self-review F8; the
// createDraftController precedent for feature-layer controller factories).
export function createOpenFetch<T>(fetcher: () => Promise<T>, onLoaded: (value: T) => void) {
  const [open, setOpen] = createSignal(false);
  const [loading, setLoading] = createSignal(false);
  const [error, setError] = createSignal("");
  let seq = 0;

  // guard captures the current lifecycle point; the returned check reports
  // whether it is still the latest (no reopen/close happened since). Exposed
  // so callers can fence their OWN follow-up requests (e.g. a conflict
  // refresh) with the same sequence.
  const guard = () => {
    const s = seq;
    return () => s === seq;
  };

  const refetch = () => {
    seq++;
    const current = guard();
    setError("");
    setLoading(true);
    void fetcher()
      .then((value) => {
        if (current()) onLoaded(value);
      })
      .catch((err: unknown) => {
        if (current()) setError(errorMessage(err));
      })
      .finally(() => {
        if (current()) setLoading(false);
      });
  };

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (next) {
      refetch();
    } else {
      seq++; // fence in-flight responses from the closed dialog
    }
  };

  return { open, loading, error, setError, guard, handleOpenChange };
}
