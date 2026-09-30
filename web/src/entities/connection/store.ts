import { createRoot, createSignal } from "solid-js";

import { ConnectError } from "@connectrpc/connect";

import type { Connection, ConnectionSummary } from "@/gen/portcullis/v1/connections_pb";
import type { ConfigDraft, EnvironmentValue, TestResult } from "@/entities/connection/model";
import { toInput } from "@/entities/connection/model";
import { connectionsClient } from "@/shared/api/client";

export type { ConfigDraft, EnvironmentValue, TestResult } from "@/entities/connection/model";

// errorMessage extracts the Connect error's message without the code prefix — server messages are generic/classified by design (ADR-0014), safe to show.
export function errorMessage(err: unknown): string {
  if (err instanceof ConnectError) {
    return err.rawMessage;
  }
  return "Request failed.";
}

// Keep list-safe summaries separate from target details. Track fetch state so initial emptiness is not shown as a completed empty list.
export type ListState = "idle" | "loading" | "ready" | "error";

const store = createRoot(() => {
  const [connections, setConnections] = createSignal<ConnectionSummary[]>([]);
  const [listError, setListError] = createSignal("");
  const [listState, setListState] = createSignal<ListState>("idle");

  // Fence reads and mutations by principal generation so previous-user responses cannot repopulate caches after reset.
  let generation = 0;
  // listRevision invalidates a list response that started before a committed mutation. The mutation response is authoritative for that one row, whereas the older list may not contain the change yet.
  let listRevision = 0;
  // loadSeq orders loads against each other: two overlapping loads share the same generation and revision, so without it the earlier (staler) request resolving last would overwrite the newer list. Only the most recently started load may apply.
  let loadSeq = 0;

  // A response from an older mutation may arrive after a newer one. Versions come from the database mutation token, so only newer state can replace the current summary for that connection.
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
    // Drop superseded generations and loads; if a mutation changed listRevision, refetch rather than apply a stale snapshot or leave loading unresolved.
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
      // Stale data must not outlive a failed refresh — the permission-denied path after a user switch is exactly this branch.
      setConnections([]);
      setListError(errorMessage(err));
      setListState("error");
    }
  }

  async function createConnection(
    displayName: string,
    environment: EnvironmentValue,
    description: string,
    cfg: ConfigDraft,
  ): Promise<void> {
    const gen = generation;
    const res = await connectionsClient.create({
      displayName,
      environment,
      description,
      config: toInput(cfg),
    });
    if (gen !== generation) return;
    listRevision++;
    // The mutation is already committed. Do not turn a later list outage into a false "create failed" dialog or invite a duplicate retry.
    const summary = res.connection;
    if (summary) {
      setConnections((current) => [summary, ...current.filter((connection) => connection.id !== summary.id)]);
      setListError("");
      return;
    }
    setListError("Connection was created, but its updated list entry was unavailable. Refresh the page.");
  }

  // updateConnection sends complete descriptor values at expectedVersion; optional config replacement is retested by the server (ADR-0014).
  async function updateConnection(
    id: string,
    displayName: string,
    environment: EnvironmentValue,
    description: string,
    expectedVersion: bigint,
    cfg?: ConfigDraft,
  ): Promise<void> {
    const gen = generation;
    const res = await connectionsClient.update({
      id,
      displayName,
      environment,
      description,
      expectedVersion,
      config: cfg ? toInput(cfg) : undefined,
    });
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
