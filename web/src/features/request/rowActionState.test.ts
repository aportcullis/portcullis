import { createRoot } from "solid-js";
import { describe, expect, it } from "vitest";

import { createRowActionRegistry } from "@/features/request/rowActionState";
import type { RowActionOperation, RowActionRegistry } from "@/features/request/rowActionState";

const createRegistry = () => createRoot(() => createRowActionRegistry());

function beginRowAction(registry: RowActionRegistry, requestId: string, operation: RowActionOperation = "request"): number {
  const admission = registry.begin(requestId, operation);
  if (admission.kind !== "started") throw new Error("Expected the row operation to be admitted.");
  const { attempt } = admission;
  return attempt;
}

describe("row action registry", () => {
  it("marks a row busy while its action runs", () => {
    const registry = createRegistry();
    registry.begin("req-1");
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
  });

  it("keeps a failure after the row is rendered again from a refreshed object", () => {
    const registry = createRegistry();
    const attempt = beginRowAction(registry, "req-1");
    registry.settle("req-1", attempt, "Execution was refused.");
    expect(registry.stateFor("req-1")).toEqual({ busy: false, error: "Execution was refused." });
  });

  it("clears an earlier failure when a later attempt succeeds", () => {
    const registry = createRegistry();
    registry.settle("req-1", beginRowAction(registry, "req-1"), "first failure");
    registry.settle("req-1", beginRowAction(registry, "req-1"), "");
    expect(registry.stateFor("req-1")).toEqual({ busy: false, error: "" });
  });

  it("keeps rows independent of each other", () => {
    const registry = createRegistry();
    registry.begin("req-1");
    registry.settle("req-2", beginRowAction(registry, "req-2"), "other row failed");
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
    expect(registry.stateFor("req-2")).toEqual({ busy: false, error: "other row failed" });
  });

  it("reports an untouched row as idle", () => {
    expect(createRegistry().stateFor("req-unknown")).toEqual({ busy: false, error: "" });
  });

  it("ignores a superseded attempt that settles late", () => {
    const registry = createRegistry();
    const stale = beginRowAction(registry, "req-1");
    registry.settle("req-1", stale, "");
    beginRowAction(registry, "req-1");
    registry.settle("req-1", stale, "late failure");
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
  });

  it("ignores an attempt that settles after the registry was cleared for a new principal", () => {
    const registry = createRegistry();
    const attempt = beginRowAction(registry, "req-1");
    registry.clear();
    registry.settle("req-1", attempt, "previous principal's failure");
    expect(registry.stateFor("req-1")).toEqual({ busy: false, error: "" });
  });

  it("does not let one row's attempt number settle another row", () => {
    const registry = createRegistry();
    const attempt = beginRowAction(registry, "req-1");
    registry.settle("req-2", attempt, "misrouted failure");
    expect(registry.stateFor("req-2")).toEqual({ busy: false, error: "" });
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
  });
});

describe("independent execution cancellation", () => {
  it("admits Stop while Execute remains pending across row refreshes", () => {
    const registry = createRegistry();
    beginRowAction(registry, "req-1");
    beginRowAction(registry, "req-1", "stopExecution");
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: true, error: "" });
  });

  it("settles Stop without releasing an Execute response that is still pending", () => {
    const registry = createRegistry();
    beginRowAction(registry, "req-1");
    const stopAttempt = beginRowAction(registry, "req-1", "stopExecution");
    registry.settle("req-1", stopAttempt, "", "stopExecution");
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: false, error: "" });
  });

  it("settles Execute without enabling a second Stop before cancellation settles", () => {
    const registry = createRegistry();
    const executionAttempt = beginRowAction(registry, "req-1");
    beginRowAction(registry, "req-1", "stopExecution");
    registry.settle("req-1", executionAttempt, "");
    expect(registry.stateFor("req-1")).toEqual({ busy: false, error: "" });
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: true, error: "" });
  });

  it("retries a failed Stop without clearing an independent Execute failure", () => {
    const registry = createRegistry();
    registry.settle("req-1", beginRowAction(registry, "req-1"), "Execution failed.");
    registry.settle("req-1", beginRowAction(registry, "req-1", "stopExecution"), "Stop failed.", "stopExecution");
    const retriedStop = beginRowAction(registry, "req-1", "stopExecution");
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: true, error: "" });
    registry.settle("req-1", retriedStop, "", "stopExecution");
    expect(registry.stateFor("req-1")).toEqual({ busy: false, error: "Execution failed." });
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: false, error: "" });
  });

  const operations: readonly RowActionOperation[] = ["request", "stopExecution"];

  it.each(operations)("refuses a duplicate pending %s operation without replacing its token", operation => {
    const registry = createRegistry();
    const admittedAttempt = beginRowAction(registry, "req-1", operation);
    expect(registry.begin("req-1", operation)).toEqual({ kind: "busy" });
    registry.settle("req-1", admittedAttempt, "Original operation failed.", operation);
    expect(registry.stateFor("req-1", operation)).toEqual({ busy: false, error: "Original operation failed." });
  });

  it("retains a Stop failure when the earlier Execute response succeeds later", () => {
    const registry = createRegistry();
    const executionAttempt = beginRowAction(registry, "req-1");
    const stopAttempt = beginRowAction(registry, "req-1", "stopExecution");
    registry.settle("req-1", stopAttempt, "Cancellation was refused.", "stopExecution");
    registry.settle("req-1", executionAttempt, "");
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: false, error: "Cancellation was refused." });
  });

  it("rejects another operation's token while both operations remain pending", () => {
    const registry = createRegistry();
    const executionAttempt = beginRowAction(registry, "req-1");
    const stopAttempt = beginRowAction(registry, "req-1", "stopExecution");
    registry.settle("req-1", executionAttempt, "Misrouted execution.", "stopExecution");
    registry.settle("req-1", stopAttempt, "Misrouted stop.");
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: true, error: "" });
  });

  it("fences both old operations after clearing and starting another principal's attempts", () => {
    const registry = createRegistry();
    const executionAttempt = beginRowAction(registry, "req-1");
    const stopAttempt = beginRowAction(registry, "req-1", "stopExecution");
    registry.clear();
    beginRowAction(registry, "req-1");
    beginRowAction(registry, "req-1", "stopExecution");
    registry.settle("req-1", executionAttempt, "Previous principal execution.");
    registry.settle("req-1", stopAttempt, "Previous principal cancellation.", "stopExecution");
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: true, error: "" });
  });

  it("ignores a late failure from a Stop attempt that was already settled and retried", () => {
    const registry = createRegistry();
    const staleStop = beginRowAction(registry, "req-1", "stopExecution");
    registry.settle("req-1", staleStop, "", "stopExecution");
    beginRowAction(registry, "req-1", "stopExecution");
    registry.settle("req-1", staleStop, "Late cancellation failure.", "stopExecution");
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: true, error: "" });
  });
});

describe("request action owner lifetime", () => {
  it("identifies both pending operations without sharing their attempt identities", () => {
    const registry = createRegistry();
    const executionAttempt = beginRowAction(registry, "req-1");
    const stopAttempt = beginRowAction(registry, "req-1", "stopExecution");
    expect(registry.isCurrentAttempt("req-1", executionAttempt)).toBe(true);
    expect(registry.isCurrentAttempt("req-1", stopAttempt, "stopExecution")).toBe(true);
  });

  it("retains the current Stop after the independent Execute response settles", () => {
    const registry = createRegistry();
    const executionAttempt = beginRowAction(registry, "req-1");
    const stopAttempt = beginRowAction(registry, "req-1", "stopExecution");
    registry.settle("req-1", executionAttempt, "");
    expect(registry.isCurrentAttempt("req-1", executionAttempt)).toBe(false);
    expect(registry.isCurrentAttempt("req-1", stopAttempt, "stopExecution")).toBe(true);
  });

  it("admits another snapshot's actions after a nonterminal authority clear", () => {
    const registry = createRegistry();
    const oldAttempt = beginRowAction(registry, "req-1");
    registry.clear();
    const currentAttempt = beginRowAction(registry, "req-1");
    expect(registry.isCurrentAttempt("req-1", oldAttempt)).toBe(false);
    expect(registry.isCurrentAttempt("req-1", currentAttempt)).toBe(true);
  });

  it("disposes every retained row and operation when their list owner leaves", () => {
    const registry = createRegistry();
    beginRowAction(registry, "req-1");
    beginRowAction(registry, "req-1", "stopExecution");
    registry.settle("req-2", beginRowAction(registry, "req-2"), "Other row failed.");
    registry.dispose();
    expect(registry.stateFor("req-1")).toEqual({ busy: false, error: "" });
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: false, error: "" });
    expect(registry.stateFor("req-2")).toEqual({ busy: false, error: "" });
  });

  it("rejects an attempt token from another row or operation", () => {
    const registry = createRegistry();
    const executionAttempt = beginRowAction(registry, "req-1");
    expect(registry.isCurrentAttempt("req-2", executionAttempt)).toBe(false);
    expect(registry.isCurrentAttempt("req-1", executionAttempt, "stopExecution")).toBe(false);
  });

  it("does not let a settled attempt authorize a later refresh", () => {
    const registry = createRegistry();
    const stopAttempt = beginRowAction(registry, "req-1", "stopExecution");
    registry.settle("req-1", stopAttempt, "", "stopExecution");
    expect(registry.isCurrentAttempt("req-1", stopAttempt, "stopExecution")).toBe(false);
  });

  it("invalidates both pending completion guards on owner disposal", () => {
    const registry = createRegistry();
    const executionAttempt = beginRowAction(registry, "req-1");
    const stopAttempt = beginRowAction(registry, "req-1", "stopExecution");
    registry.dispose();
    expect(registry.isCurrentAttempt("req-1", executionAttempt)).toBe(false);
    expect(registry.isCurrentAttempt("req-1", stopAttempt, "stopExecution")).toBe(false);
  });

  it("refuses both kinds of new writes after owner disposal", () => {
    const registry = createRegistry();
    registry.dispose();
    expect(registry.begin("req-1")).toEqual({ kind: "disposed" });
    expect(registry.begin("req-1", "stopExecution")).toEqual({ kind: "disposed" });
  });

  it("cannot restore obsolete output by settling after disposal", () => {
    const registry = createRegistry();
    const executionAttempt = beginRowAction(registry, "req-1");
    const stopAttempt = beginRowAction(registry, "req-1", "stopExecution");
    registry.dispose();
    registry.settle("req-1", executionAttempt, "Obsolete execution failure.");
    registry.settle("req-1", stopAttempt, "Obsolete Stop failure.", "stopExecution");
    expect(registry.stateFor("req-1")).toEqual({ busy: false, error: "" });
    expect(registry.stateFor("req-1", "stopExecution")).toEqual({ busy: false, error: "" });
  });

  it("cannot reopen a disposed owner by clearing or disposing it again", () => {
    const registry = createRegistry();
    registry.dispose();
    registry.clear();
    registry.dispose();
    expect(registry.begin("req-1")).toEqual({ kind: "disposed" });
    expect(registry.begin("req-1", "stopExecution")).toEqual({ kind: "disposed" });
  });
});
