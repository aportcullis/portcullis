import { createRoot, createSignal } from "solid-js";

import { ConnectError } from "@connectrpc/connect";

import type { Connection, ConnectionSummary } from "@/gen/portcullis/v1/connections_pb";
import type { ConfigDraft, TestResult } from "@/entities/connection/model";
import { toInput } from "@/entities/connection/model";
import { connectionsClient } from "@/shared/api/client";

export type { ConfigDraft, TestResult } from "@/entities/connection/model";

// errorMessage extracts the Connect error's message without the code prefix —
// server messages are generic/classified by design (ADR-0014), safe to show.
export function errorMessage(err: unknown): string {
  if (err instanceof ConnectError) {
    return err.rawMessage;
  }
  return "Request failed.";
}

// Module-level store: the list-safe connection summaries (archived included —
// the page renders the status) plus mutation actions. Detail belongs to the
// connections.get path; list-only callers never receive target coordinates.
// ListState tracks the list fetch lifecycle so the UI can tell "not loaded
// yet" apart from "loaded and genuinely empty": the store's initial value is an
// empty array, and rendering an empty state before the first response would
// wrongly tell an operator with existing connections that there are none
// (external review).
export type ListState = "idle" | "loading" | "ready" | "error";

const store = createRoot(() => {
  const [connections, setConnections] = createSignal<ConnectionSummary[]>([]);
  const [listError, setListError] = createSignal("");
  const [listState, setListState] = createSignal<ListState>("idle");

  // The store outlives sessions (module lifetime), so it must not outlive the
  // USER. A monotonic generation, bumped by resetConnections, fences every
  // in-flight read and mutation: a response that resolves after a reset (a slow
  // request from the previous principal) is dropped instead of repopulating the
  // cache for whoever is logged in now. resetConnections is called from the app
  // layer on a principal change (it owns the session→entity wiring; this entity
  // does not import the session entity).
  let generation = 0;
  // listRevision invalidates a list response that started before a committed
  // mutation. The mutation response is authoritative for that one row, whereas
  // the older list may not contain the change yet.
  let listRevision = 0;
  // loadSeq orders loads against each other: two overlapping loads share the
  // same generation and revision, so without it the earlier (staler) request
  // resolving last would overwrite the newer list (external review). Only the
  // most recently started load may apply.
  let loadSeq = 0;

  // A response from an older mutation may arrive after a newer one. Versions
  // come from the database mutation token, so only newer state can replace the
  // current summary for that connection.
  function applySummary(summary: ConnectionSummary): void {
    setConnections((current) =>
      current.map((connection) =>
        connection.id === summary.id && connection.version < summary.version ? summary : connection,
      ),
    );
  }

  function resetConnections(): void {
    generation++;
    listRevision++;
    setConnections([]);
    setListError("");
    setListState("idle");
  }

  async function loadConnections(): Promise<void> {
    const gen = generation;
    const revision = listRevision;
    const seq = ++loadSeq;
    setListState("loading");
    // Guard order on completion (store.test.ts pins these interleavings with
    // deferred promises): a stale gen/seq means a reset or a
    // newer load OWNS the state now, so this response is dropped outright. A
    // changed listRevision means a mutation committed mid-flight: the snapshot
    // is stale but this load still owns the state, and it must not park it in
    // "loading" forever (external review) — the mutation response was
    // authoritative only for its own row, not the whole list, so the honest
    // exit is a refetch (bounded: each retry consumes one revision bump).
    try {
      const res = await connectionsClient.list({ includeArchived: true });
      if (gen !== generation || seq !== loadSeq) return;
      if (revision !== listRevision) {
        void loadConnections();
        return;
      }
      setConnections((current) =>
        res.connections.map((summary) => {
          const local = current.find((connection) => connection.id === summary.id);
          return local && local.version > summary.version ? local : summary;
        }),
      );
      setListError("");
      setListState("ready");
    } catch (err) {
      if (gen !== generation || seq !== loadSeq) return;
      if (revision !== listRevision) {
        void loadConnections();
        return;
      }
      // Stale data must not outlive a failed refresh — the permission-denied
      // path after a user switch is exactly this branch.
      setConnections([]);
      setListError(errorMessage(err));
      setListState("error");
    }
  }

  async function createConnection(displayName: string, cfg: ConfigDraft): Promise<void> {
    const gen = generation;
    const res = await connectionsClient.create({ displayName, config: toInput(cfg) });
    if (gen !== generation) return;
    listRevision++;
    // The mutation is already committed. Do not turn a later list outage into a
    // false "create failed" dialog or invite a duplicate retry.
    const summary = res.connection;
    if (summary) {
      setConnections((current) => [summary, ...current.filter((connection) => connection.id !== summary.id)]);
      setListError("");
      return;
    }
    setListError("Connection was created, but its updated list entry was unavailable. Refresh the page.");
  }

  // updateConnection renames when cfg is absent, or replaces the full config
  // (server re-tests before persisting — ADR-0014's two update flows).
  async function updateConnection(id: string, displayName: string, cfg?: ConfigDraft): Promise<void> {
    const gen = generation;
    const res = await connectionsClient.update({ id, displayName, config: cfg ? toInput(cfg) : undefined });
    if (gen !== generation) return;
    listRevision++;
    const summary = res.connection;
    if (summary) {
      applySummary(summary);
      setListError("");
      return;
    }
    setListError("Connection was updated, but its updated list entry was unavailable. Refresh the page.");
  }

  async function archiveConnection(id: string): Promise<void> {
    const gen = generation;
    const res = await connectionsClient.archive({ id });
    if (gen !== generation) return;
    listRevision++;
    const summary = res.connection;
    if (summary) {
      applySummary(summary);
      setListError("");
      return;
    }
    setListError("Connection was archived, but its updated list entry was unavailable. Refresh the page.");
  }

  async function testSaved(id: string): Promise<TestResult> {
    const res = await connectionsClient.test({ target: { case: "id", value: id } });
    return { ok: res.ok, message: res.message };
  }

  async function getConnection(id: string): Promise<Connection | undefined> {
    const res = await connectionsClient.get({ id });
    return res.connection;
  }

  async function testDraft(cfg: ConfigDraft): Promise<TestResult> {
    const res = await connectionsClient.test({ target: { case: "config", value: toInput(cfg) } });
    return { ok: res.ok, message: res.message };
  }

  return {
    connections,
    listError,
    listState,
    resetConnections,
    loadConnections,
    createConnection,
    updateConnection,
    archiveConnection,
    getConnection,
    testSaved,
    testDraft,
  };
});

export const {
  connections,
  listError,
  listState,
  resetConnections,
  loadConnections,
  createConnection,
  updateConnection,
  archiveConnection,
  getConnection,
  testSaved,
  testDraft,
} = store;
