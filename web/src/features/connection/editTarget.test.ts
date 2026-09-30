import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";

import { ConnectionSummarySchema } from "@/gen/portcullis/v1/connections_pb";
import { resolveEditTarget } from "@/features/connection/editTarget";

const summary = (id: string, version: bigint, displayName = id) =>
  create(ConnectionSummarySchema, { id, version, displayName, dbType: "postgresql" });

describe("resolveEditTarget", () => {
  it("follows the row through a refresh, so the retry carries the current token", () => {
    const captured = summary("conn-1", 3n);
    const refreshed = [summary("conn-1", 4n), summary("conn-2", 1n)];
    expect(resolveEditTarget(refreshed, captured)?.version).toBe(4n);
  });

  it("falls back to the captured row when the list could not be reloaded", () => {
    // A failed load empties the store; closing the form on top of that error would take the operator's input with it.
    const captured = summary("conn-1", 3n, "Primary");
    const resolved = resolveEditTarget([], captured);
    expect(resolved?.id).toBe("conn-1");
    expect(resolved?.displayName).toBe("Primary");
  });

  it("resolves nothing when no editor is open", () => {
    expect(resolveEditTarget([summary("conn-1", 1n)], undefined)).toBeUndefined();
  });

  it("does not confuse a different connection for the one being edited", () => {
    const captured = summary("conn-1", 3n);
    expect(resolveEditTarget([summary("conn-2", 9n)], captured)?.id).toBe("conn-1");
    expect(resolveEditTarget([summary("conn-2", 9n)], captured)?.version).toBe(3n);
  });
});
