import { createSignal } from "solid-js";
import { Code, ConnectError } from "@connectrpc/connect";

import { managementError } from "@/entities/administration/errors";
import { createDialogSession } from "@/shared/lib/dialogSession";

/** Owns one route's authorized result; reset fences all outstanding work. */
export function createManagementQuery<Result>(onSessionExpired: () => void = () => {}) {
  const [data, setData] = createSignal<Result>();
  const [error, setError] = createSignal("");
  const [loading, setLoading] = createSignal(false);
  const lifetime = createDialogSession();
  /** Clears protected results and immediately releases pending presentation. */
  const reset = () => { lifetime.discardSession(); setData(undefined); setError(""); setLoading(false); };
  /** Applies only the latest route-owned authorized result and never retains denied rows. */
  const load = async (work: () => Promise<Result>) => {
    reset(); setLoading(true);
    const result = await lifetime.runInSession(work);
    if (result.status === "superseded") return;
    setLoading(false);
    if (result.status === "ok") setData(() => result.value);
    else {
      setError(managementError(result.error));
      if (result.error instanceof ConnectError && result.error.code === Code.Unauthenticated) onSessionExpired();
    }
  };
  return { data, error, loading, load, reset };
}
