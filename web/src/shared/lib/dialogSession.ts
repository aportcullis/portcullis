/** Describes completion or supersession of asynchronous dialog work. */
export type SessionOutcome<T> =
  | { status: "ok"; value: T }
  | { status: "failed"; error: unknown }
  | { status: "superseded" };

/** Guards asynchronous callbacks against a dialog session changing. */
export function createDialogSession() {
  let current = 0;

  /** Invalidates callbacks captured in the current session. */
  const discardSession = () => {
    current++;
  };

  /** Returns a predicate checking whether the captured session is still current. */
  const captureSession = () => {
    const captured = current;
    return () => captured === current;
  };

  /** Reports asynchronous work as superseded when its session changes, without cancelling the work. */
  const runInSession = async <T>(work: () => Promise<T>): Promise<SessionOutcome<T>> => {
    const isSameSession = captureSession();
    try {
      const value = await work();
      return isSameSession() ? { status: "ok", value } : { status: "superseded" };
    } catch (error: unknown) {
      return isSameSession() ? { status: "failed", error } : { status: "superseded" };
    }
  };

  return { discardSession, captureSession, runInSession };
}
