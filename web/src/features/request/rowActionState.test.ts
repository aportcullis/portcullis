import { createRoot } from "solid-js";
import { describe, expect, it } from "vitest";

import { createRowActionRegistry } from "@/features/request/rowActionState";

// Solid's <For> recreates a row when the refreshed list carries a new object for it, so an Execute that refreshes the list would lose its pending and error state if the row owned it.
const createRegistry = () => createRoot(() => createRowActionRegistry());

describe("row action registry", () => {
  it("marks a row busy while its action runs", () => {
    const registry = createRegistry();
    registry.begin("req-1");
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
  });

  it("keeps a failure after the row is rendered again from a refreshed object", () => {
    const registry = createRegistry();
    const attempt = registry.begin("req-1");
    registry.settle("req-1", attempt, "Execution was refused.");
    // A recreated row looks its state up by ID again.
    expect(registry.stateFor("req-1")).toEqual({ busy: false, error: "Execution was refused." });
  });

  it("clears an earlier failure when a later attempt succeeds", () => {
    const registry = createRegistry();
    registry.settle("req-1", registry.begin("req-1"), "first failure");
    registry.settle("req-1", registry.begin("req-1"), "");
    expect(registry.stateFor("req-1")).toEqual({ busy: false, error: "" });
  });

  it("keeps rows independent of each other", () => {
    const registry = createRegistry();
    registry.begin("req-1");
    registry.settle("req-2", registry.begin("req-2"), "other row failed");
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
    expect(registry.stateFor("req-2")).toEqual({ busy: false, error: "other row failed" });
  });

  it("reports an untouched row as idle", () => {
    expect(createRegistry().stateFor("req-unknown")).toEqual({ busy: false, error: "" });
  });

  it("ignores a superseded attempt that settles late", () => {
    const registry = createRegistry();
    const stale = registry.begin("req-1");
    registry.begin("req-1");
    registry.settle("req-1", stale, "late failure");
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
  });

  it("ignores an attempt that settles after the registry was cleared for a new principal", () => {
    const registry = createRegistry();
    const attempt = registry.begin("req-1");
    registry.clear();
    registry.settle("req-1", attempt, "previous principal's failure");
    expect(registry.stateFor("req-1")).toEqual({ busy: false, error: "" });
  });

  it("does not let one row's attempt number settle another row", () => {
    const registry = createRegistry();
    const attempt = registry.begin("req-1");
    registry.settle("req-2", attempt, "misrouted failure");
    expect(registry.stateFor("req-2")).toEqual({ busy: false, error: "" });
    expect(registry.stateFor("req-1")).toEqual({ busy: true, error: "" });
  });
});
