import { createEffect, createMemo, createSignal, on, onCleanup } from "solid-js";
import { Code, ConnectError } from "@connectrpc/connect";
import { hasPermission, session, sessionAuthorizationKey } from "@/entities/session/store";
import { createBackgroundAuthorizationContext } from "@/shared/api/authorization";
import { requestsClient } from "@/shared/api/client";
import { accessRequestTotalCountSchema } from "@/shared/api/accessRequestValidation";

export type PendingCountState = { kind: "unavailable" } | { kind: "ready"; totalCount: bigint };

/** Polls the visible pending count and fences responses after a session change. */
export function createPendingRequestCount() {
  const [count, setCount] = createSignal<PendingCountState>({ kind: "unavailable" });
  const authorizationKey = createMemo(sessionAuthorizationKey);
  createEffect(on(authorizationKey, capturedAuthorizationKey => {
    const principal = session();
    setCount({ kind: "unavailable" });
    if (principal.status !== "authenticated" || !hasPermission("requests.list") || !hasPermission("requests.approve")) return;
    let active = true;
    let inFlight = false;
    const refreshPendingCount = async () => {
      if (!active || document.hidden || inFlight) return;
      const readPrincipal = session();
      const isCurrentPendingRead = () => active &&
        authorizationKey() === capturedAuthorizationKey && session() === readPrincipal;
      if (!isCurrentPendingRead()) return;
      inFlight = true;
      try {
        const { totalCount } = await requestsClient.list(
          { state: "pending", page: 1, pageSize: 10 },
          { contextValues: createBackgroundAuthorizationContext(isCurrentPendingRead) },
        );
        if (!isCurrentPendingRead()) return;
        const validatedCount = accessRequestTotalCountSchema.safeParse(totalCount);
        setCount(validatedCount.success ? { kind: "ready", totalCount: validatedCount.data } : { kind: "unavailable" });
      } catch (error) {
        if (isCurrentPendingRead() && error instanceof ConnectError && (
          error.code === Code.PermissionDenied || error.code === Code.Unauthenticated
        )) setCount({ kind: "unavailable" });
      }
      finally { inFlight = false; }
    };
    void refreshPendingCount();
    const timer = setInterval(() => void refreshPendingCount(), 30_000);
    const refreshPendingCountAfterReconnect = () => void refreshPendingCount();
    window.addEventListener("online", refreshPendingCountAfterReconnect);
    document.addEventListener("visibilitychange", refreshPendingCountAfterReconnect);
    onCleanup(() => {
      active = false; clearInterval(timer);
      window.removeEventListener("online", refreshPendingCountAfterReconnect);
      document.removeEventListener("visibilitychange", refreshPendingCountAfterReconnect);
    });
  }));
  return count;
}
