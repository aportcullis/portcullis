import { createEffect, createSignal, onCleanup } from "solid-js";
import { hasPermission, session } from "@/entities/session/store";
import { requestsClient } from "@/shared/api/client";

/** Polls the visible pending count and fences responses after a session change. */
export function createPendingRequests() {
  const [count, setCount] = createSignal(0n);
  createEffect(() => {
    const principal = session();
    setCount(0n);
    if (principal.status !== "authenticated" || !hasPermission("requests.list") || !hasPermission("requests.approve")) return;
    let active = true;
    let inFlight = false;
    const refresh = async () => {
      if (document.hidden || inFlight) return;
      inFlight = true;
      try {
        const response = await requestsClient.list({ state: "pending", page: 1, pageSize: 10 });
        if (active && session() === principal) setCount(response.totalCount);
      } catch { /* Retain the last count; the requests page exposes retrieval errors. */ }
      finally { inFlight = false; }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), 30_000);
    const reconnect = () => void refresh();
    window.addEventListener("online", reconnect);
    document.addEventListener("visibilitychange", reconnect);
    onCleanup(() => {
      active = false; clearInterval(timer);
      window.removeEventListener("online", reconnect);
      document.removeEventListener("visibilitychange", reconnect);
    });
  });
  return count;
}
