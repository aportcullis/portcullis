import { create } from "@bufbuild/protobuf";
import { describe, expect, it, vi } from "vitest";

import type { ConnectionSummary } from "@/gen/portcullis/v1/connections_pb";
import { ConnectionSummarySchema } from "@/gen/portcullis/v1/connections_pb";

// The store's race guards (generation / listRevision / loadSeq) exist for interleavings Playwright cannot schedule deterministically, so they are pinned here with deferred promises.
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
  let resolve = (_value: T): void => { throw new Error("Deferred resolver is not initialized"); };
  let reject = (_reason: unknown): void => { throw new Error("Deferred rejection is not initialized"); };
  const promise = new Promise<T>((settle, fail) => {
    resolve = settle;
    reject = fail;
  });
  return { promise, resolve, reject };
}


const summary = (id: string, version: bigint): ConnectionSummary =>
  create(ConnectionSummarySchema, { id, displayName: id, dbType: "postgresql", version });

const fresh = () => {
  vi.clearAllMocks();
  resetConnections();
};

describe("connection list store", () => {
  it("drops an older overlapping load that resolves after a newer one", async () => {
    fresh();
    const earlierList = deferred<{ connections: ConnectionSummary[] }>();
    const laterList = deferred<{ connections: ConnectionSummary[] }>();
    client.list.mockReturnValueOnce(earlierList.promise).mockReturnValueOnce(laterList.promise);

    const earlierLoad = loadConnections();
    const laterLoad = loadConnections();
    laterList.resolve({ connections: [summary("newer", 1n)] });
    await laterLoad;
    earlierList.resolve({ connections: [summary("stale", 1n)] });
    await earlierLoad;

    expect(connections().map((connection) => connection.id)).toEqual(["newer"]);
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

    // The stale snapshot (started before the create) resolves last; the store must refetch rather than apply it or park in "loading" forever.
    client.list.mockResolvedValueOnce({
      connections: [summary("created", 1n), summary("preexisting", 1n)],
    });
    staleLoad.resolve({ connections: [] });
    await load;
    await vi.waitFor(() => {
      expect(listState()).toBe("ready");
    });
    expect(connections().map((connection) => connection.id).sort()).toEqual(["created", "preexisting"]);
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
    expect(connections().map((connection) => connection.id)).toEqual(["back"]);
  });
});

// A list load merges by ID: a row the client already holds at a newer mutation version survives a list snapshot that has not caught up.
describe("connection list merge by version", () => {
  async function holdRows(...rows: ConnectionSummary[]): Promise<void> {
    fresh();
    client.list.mockResolvedValueOnce({ connections: rows });
    await loadConnections();
  }

  const versions = () => connections().map((connection) => `${connection.id}@${connection.version}`);

  it("keeps a held row whose version is newer than the snapshot's", async () => {
    await holdRows(summary("primary", 5n), summary("replica", 1n));
    client.list.mockResolvedValueOnce({ connections: [summary("primary", 4n), summary("replica", 2n)] });
    await loadConnections();
    expect(versions()).toEqual(["primary@5", "replica@2"]);
  });

  it("takes the snapshot's row when it is newer", async () => {
    await holdRows(summary("primary", 1n));
    client.list.mockResolvedValueOnce({ connections: [summary("primary", 2n)] });
    await loadConnections();
    expect(versions()).toEqual(["primary@2"]);
  });

  it("takes the snapshot's row at an equal version", async () => {
    await holdRows(summary("primary", 3n));
    const renamed = create(ConnectionSummarySchema, { id: "primary", displayName: "renamed", version: 3n });
    client.list.mockResolvedValueOnce({ connections: [renamed] });
    await loadConnections();
    expect(connections()[0]?.displayName).toBe("renamed");
  });

  it("follows the snapshot's order and adds rows the client did not hold", async () => {
    await holdRows(summary("primary", 1n));
    client.list.mockResolvedValueOnce({ connections: [summary("added", 1n), summary("primary", 1n)] });
    await loadConnections();
    expect(versions()).toEqual(["added@1", "primary@1"]);
  });

  it("drops a held row the snapshot no longer lists, even at a newer version", async () => {
    await holdRows(summary("primary", 9n), summary("replica", 1n));
    client.list.mockResolvedValueOnce({ connections: [summary("replica", 1n)] });
    await loadConnections();
    expect(versions()).toEqual(["replica@1"]);
  });

  it("does not let a newer held row of one ID replace another ID", async () => {
    await holdRows(summary("primary", 9n));
    client.list.mockResolvedValueOnce({ connections: [summary("replica", 1n)] });
    await loadConnections();
    expect(versions()).toEqual(["replica@1"]);
  });

  it("keeps duplicate snapshot rows as the server sent them", async () => {
    await holdRows();
    client.list.mockResolvedValueOnce({ connections: [summary("primary", 1n), summary("primary", 1n)] });
    await loadConnections();
    expect(versions()).toEqual(["primary@1", "primary@1"]);
  });
});
