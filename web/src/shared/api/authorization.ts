/** Classifies a server refusal without interpreting its private diagnostic text. */
export type AuthorizationFailure = "session" | "permission";
const observers = new Set<(failure: AuthorizationFailure) => void>();

/** Registers the composition root's session refresh without an upward dependency from transport. */
export function observeAuthorizationFailures(observer: (failure: AuthorizationFailure) => void): () => void {
  observers.add(observer);
  return () => { observers.delete(observer); };
}

/** Notifies session coordination of a refused protected operation without replaying it. */
export function notifyAuthorizationFailure(failure: AuthorizationFailure): void {
  for (const observer of observers) observer(failure);
}
