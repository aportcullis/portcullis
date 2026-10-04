import { createSignal } from "solid-js";

/** Pending and failure state of one request row's action, independent of the row object that rendered it. */
export type RowActionState = { busy: boolean; error: string };

type TrackedRowAction = RowActionState & { attempt: number };

const idleRowAction: RowActionState = { busy: false, error: "" };

/** Holds row action state by request ID so a list refresh that recreates rows keeps it. */
export function createRowActionRegistry() {
  const [actions, setActions] = createSignal<ReadonlyMap<string, TrackedRowAction>>(new Map());
  // Attempts are numbered across all rows and clears, so a number from a cleared registry or another row never matches.
  let nextAttempt = 0;

  /** Returns the current action state of the request's row. */
  const stateFor = (requestId: string): RowActionState => {
    const tracked = actions().get(requestId);
    return tracked ? { busy: tracked.busy, error: tracked.error } : idleRowAction;
  };

  /** Marks the row busy and returns the attempt number its completion must present. */
  const begin = (requestId: string): number => {
    const attempt = ++nextAttempt;
    setActions((current) => new Map(current).set(requestId, { busy: true, error: "", attempt }));
    return attempt;
  };

  /** Records the outcome of the row's current attempt; an empty error means success, and a superseded attempt changes nothing. */
  const settle = (requestId: string, attempt: number, error: string): void => {
    if (actions().get(requestId)?.attempt !== attempt) return;
    setActions((current) => new Map(current).set(requestId, { busy: false, error, attempt }));
  };

  /** Forgets every row's state, fencing attempts still in flight. */
  const clear = (): void => {
    nextAttempt++;
    setActions(new Map());
  };

  return { stateFor, begin, settle, clear };
}

export type RowActionRegistry = ReturnType<typeof createRowActionRegistry>;
