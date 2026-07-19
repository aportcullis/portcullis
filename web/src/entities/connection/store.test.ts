import { describe, expect, it, vi } from "vitest";

import type { ConnectionSummary } from "@/gen/portcullis/v1/connections_pb";

// The store's race guards (generation / listRevision / loadSeq) exist for
// interleavings Playwright cannot schedule deterministically, so they are
// pinned here with deferred promises (external review — 11th round).
const client = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  archive: vi.fn(),
  test: vi.fn(),
  get: vi.fn(),
}));
vi.mock("@/shared/api/client", () => ({ connectionsClient: client }));

import { emptyDraft } from "@/entities/connection/model";
import {
  connections,
  createConnection,
  listError,
  listState,
  loadConnections,
  resetConnections,
} from "@/entities/connection/store";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

// A structurally sufficient summary; the store never touches proto internals.
const summary = (id: string, version: bigint): ConnectionSummary =>
  ({ id, displayName: id, dbType: "postgresql", version }) as ConnectionSummary;

const fresh = () => {
  vi.clearAllMocks();
  resetConnections();
};

describe("connection list store", () => {
  it("drops an older overlapping load that resolves after a newer one", async () => {
    fresh();
    const a = deferred<{ connections: ConnectionSummary[] }>();
    const b = deferred<{ connections: ConnectionSummary[] }>();
    client.list.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);

    const loadA = loadConnections();
    const loadB = loadConnections();
    b.resolve({ connections: [summary("newer", 1n)] });
    await loadB;
    a.resolve({ connections: [summary("stale", 1n)] });
    await loadA;

    expect(connections().map((c) => c.id)).toEqual(["newer"]);
    expect(listState()).toBe("ready");
  });

  it("refetches when a mutation invalidates an in-flight load, ending ready", async () => {
    fresh();
    const staleLoad = deferred<{ connections: ConnectionSummary[] }>();
    client.list.mockReturnValueOnce(staleLoad.promise);
    const load = loadConnections();

    client.create.mockResolvedValueOnce({ connection: summary("created", 1n) });
    await createConnection("created", "development", "", emptyDraft());
    expect(listState()).toBe("loading");

    // The stale snapshot (started before the create) resolves last; the store
    // must refetch rather than apply it or park in "loading" forever.
    client.list.mockResolvedValueOnce({
      connections: [summary("created", 1n), summary("preexisting", 1n)],
    });
    staleLoad.resolve({ connections: [] });
    await load;
    await vi.waitFor(() => {
      expect(listState()).toBe("ready");
    });
    expect(connections().map((c) => c.id).sort()).toEqual(["created", "preexisting"]);
  });

  it("reports a failed load and recovers on the next one", async () => {
    fresh();
    client.list.mockRejectedValueOnce(new Error("boom"));
    await loadConnections();
    expect(listState()).toBe("error");
    expect(listError()).not.toBe("");
    expect(connections()).toEqual([]);

    client.list.mockResolvedValueOnce({ connections: [summary("back", 1n)] });
    await loadConnections();
    expect(listState()).toBe("ready");
    expect(listError()).toBe("");
    expect(connections().map((c) => c.id)).toEqual(["back"]);
  });
});
