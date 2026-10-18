import { createSignal, onCleanup } from "solid-js";
import { Code, ConnectError } from "@connectrpc/connect";

import { isManagementAdmissionFailure, managementError } from "@/entities/administration/errors";
import { load } from "@/entities/session/store";
import { createDialogSession } from "@/shared/lib/dialogSession";

/** Owns mutation feedback and fences every completion to the current route lifetime. */
export function createManagementAction(onAdmissionFailure: () => void = () => {}) {
  const lifetime = createDialogSession();
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");
  const [message, setMessage] = createSignal("");
  const [locked, setLocked] = createSignal(false);
  onCleanup(() => { lifetime.discardSession(); setBusy(false); });
  /** Runs a single user intent without automatic replay of ambiguous mutations. */
  async function run<Result>(work: (isCurrent: () => boolean) => Promise<Result>, success = "Saved.") {
    if (busy() || locked()) return;
    setBusy(true); setError(""); setMessage("");
    const isCurrent = lifetime.captureSession();
    const outcome = await lifetime.runInSession(() => work(isCurrent));
    if (outcome.status === "superseded") return;
    setBusy(false);
    if (outcome.status === "failed") {
      const failure = outcome.error;
      if (isManagementAdmissionFailure(failure)) { setLocked(true); onAdmissionFailure(); void load(); }
      if (failure instanceof ConnectError && [Code.Aborted, Code.Unavailable, Code.Unknown, Code.DeadlineExceeded, Code.Internal].includes(failure.code)) setLocked(true);
      setError(managementError(failure)); return;
    }
    setMessage(success);
    return { value: outcome.value };
  }
  return { run, busy, error, message, locked };
}
