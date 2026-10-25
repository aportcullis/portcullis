import { createContextKey, createContextValues } from "@connectrpc/connect";

type AuthorizationCallScope =
  | { kind: "interaction" }
  | { kind: "owned-interaction"; isCurrent: () => boolean }
  | { kind: "background-read"; isCurrent: () => boolean };
type AuthorizationRefusalPurpose = "interaction" | "background-read";

export type AuthorizationFailure =
  | { kind: "session" }
  | { kind: "permission"; scope: AuthorizationRefusalPurpose };

export const authorizationCallScope = createContextKey<AuthorizationCallScope>({ kind: "interaction" });

/** Keeps a background refusal inside its live read lifetime; this grants no authority. */
export function createBackgroundAuthorizationContext(isCurrent: () => boolean) {
  return createContextValues().set(authorizationCallScope, { kind: "background-read", isCurrent });
}

/** Fences refusal notification to the caller-owned interaction without changing mutation settlement. */
export function createOwnedInteractionAuthorizationContext(isCurrent: () => boolean) {
  return createContextValues().set(authorizationCallScope, { kind: "owned-interaction", isCurrent });
}

const observers = new Set<(failure: AuthorizationFailure) => void>();

/** Subscribes to call refusals without coupling transport to UI ownership. */
export function observeAuthorizationFailures(observer: (failure: AuthorizationFailure) => void): () => void {
  observers.add(observer);
  return () => { observers.delete(observer); };
}

/** Notifies session coordination of a refused protected operation without replaying it. */
export function notifyAuthorizationFailure(failure: AuthorizationFailure): void {
  for (const observer of observers) observer(failure);
}
