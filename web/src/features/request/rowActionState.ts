import { createSignal } from "solid-js";

/** Pending and failure state of one request row's action, independent of the row object that rendered it. */
export type RowActionState = { busy: boolean; error: string };

export type RowActionOperation = "request" | "stopExecution";

type TrackedRowAction = { kind: "idle" } | { kind: "pending"; attempt: number } |
  { kind: "settled"; attempt: number; error: string };
type TrackedRowOperations = Readonly<Record<RowActionOperation, TrackedRowAction>>;
type RowActionAdmission = { kind: "started"; attempt: number } | { kind: "busy" } | { kind: "disposed" };

const idleRowAction: RowActionState = { busy: false, error: "" };
const idleRowOperations: TrackedRowOperations = {
  request: { kind: "idle" },
  stopExecution: { kind: "idle" },
};

/** Holds row action state by request ID so a list refresh that recreates rows keeps it. */
export function createRowActionRegistry() {
  const [actions, setActions] = createSignal<ReadonlyMap<string, TrackedRowOperations>>(new Map());
  // Attempts are numbered across all rows and clears, so a number from a cleared registry or another row never matches.
  let nextAttempt = 0;
  let disposed = false;

  const readTrackedRowAction = (requestId: string, operation: RowActionOperation): TrackedRowAction => {
    const rowOperations = actions().get(requestId);
    return rowOperations === undefined ? idleRowOperations[operation] : rowOperations[operation];
  };

  const recordRowAction = (requestId: string, operation: RowActionOperation, action: TrackedRowAction): void => {
    setActions(current => {
      const rowOperations = current.get(requestId);
      const retainedOperations = rowOperations === undefined ? idleRowOperations : rowOperations;
      return new Map(current).set(requestId, { ...retainedOperations, [operation]: action });
    });
  };

  const stateFor = (requestId: string, operation: RowActionOperation = "request"): RowActionState => {
    const tracked = readTrackedRowAction(requestId, operation);
    const { kind } = tracked;
    if (kind === "idle") return idleRowAction;
    if (kind === "pending") return { busy: true, error: "" };
    const { error } = tracked;
    return { busy: false, error };
  };

  const isCurrentAttempt = (requestId: string, attempt: number, operation: RowActionOperation = "request"): boolean => {
    if (disposed) return false;
    const tracked = readTrackedRowAction(requestId, operation);
    if (tracked.kind !== "pending") return false;
    const { attempt: pendingAttempt } = tracked;
    return pendingAttempt === attempt;
  };

  const begin = (requestId: string, operation: RowActionOperation = "request"): RowActionAdmission => {
    if (disposed) return { kind: "disposed" };
    if (readTrackedRowAction(requestId, operation).kind === "pending") return { kind: "busy" };
    const attempt = ++nextAttempt;
    recordRowAction(requestId, operation, { kind: "pending", attempt });
    return { kind: "started", attempt };
  };

  const settle = (requestId: string, attempt: number, error: string, operation: RowActionOperation = "request"): void => {
    if (!isCurrentAttempt(requestId, attempt, operation)) return;
    recordRowAction(requestId, operation, { kind: "settled", attempt, error });
  };

  const clear = (): void => {
    nextAttempt++;
    setActions(new Map());
  };

  /** Permanently ends the owning view's action lifetime without aborting server work. */
  const dispose = (): void => {
    if (disposed) return;
    disposed = true;
    clear();
  };

  return { stateFor, begin, settle, clear, dispose, isCurrentAttempt };
}

export type RowActionRegistry = ReturnType<typeof createRowActionRegistry>;
