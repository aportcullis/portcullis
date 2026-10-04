import { createRoot, createSignal } from "solid-js";

import { Code, ConnectError } from "@connectrpc/connect";

import type {
  AccessRequest,
  RequestableConnection,
  TypedParam,
} from "@/gen/portcullis/v1/access_requests_pb";
import { requestsClient } from "@/shared/api/client";

export type ListState = "idle" | "loading" | "ready" | "error";

// TargetLoad is what a target read concluded — see loadTargets. "ok" carries the server's answer (possibly an empty list), "error" means the question never got answered, "superseded" means we dropped the read ourselves.
export type TargetLoad = "ok" | "error" | "superseded";

// errorMessage extracts the Connect error's message; server messages are generic/classified by design and never echo SQL or parameters (§8.1).
export function errorMessage(err: unknown): string {
  if (err instanceof ConnectError) {
    return err.rawMessage;
  }
  return "Request failed.";
}

/** Reports whether a failure means the caller may no longer see the list at all. */
function isAuthorizationFailure(err: unknown): boolean {
  return err instanceof ConnectError && (err.code === Code.PermissionDenied || err.code === Code.Unauthenticated);
}

/** Identifies one list query so retained rows are only ever shown for the query they answered. */
function listQueryKey(page: number, pageSize: number, stateFilter: string): string {
  return `${page}/${pageSize}/${stateFilter}`;
}

// The access-request list store, mirroring the connection store's guard discipline (generation / listRevision / loadSeq) so a slow response from a previous principal or a pre-mutation snapshot can never repopulate the cache for the current user (ADR-0018 UI). The server owns visibility scope; the store just renders what it returns.
const store = createRoot(() => {
  const [accessRequests, setAccessRequests] = createSignal<AccessRequest[]>([]);
  // The connections this caller may target. Served by the access-request RPC (requests.create), NOT the admin connection list — the seeded requester and approver roles hold no connections.list (ADR-0008/0018).
  const [targets, setTargets] = createSignal<RequestableConnection[]>([]);
  const [listError, setListError] = createSignal("");
  const [listState, setListState] = createSignal<ListState>("idle");
  const [listStale, setListStale] = createSignal(false);
  const [targetError, setTargetError] = createSignal("");
  const [targetState, setTargetState] = createSignal<ListState>("idle");
  // targetsStale marks the cached targets as possibly out of date WITHOUT discarding them: the picker may be on screen, and a user mid-edit must not lose their options (or their selection) because a request was refused.
  const [targetsStale, setTargetsStale] = createSignal(false);
  // Whether this principal may read the request list. Set by the composition root from the session's permissions — an entity must not import another entity (frontend.md), and this is the same wiring the cache reset uses.
  const [mayListAccessRequests, setMayListAccessRequests] = createSignal(true);
  const [page, setPage] = createSignal(1);
  const [pageSize, setPageSize] = createSignal(20);
  const [totalCount, setTotalCount] = createSignal(0n);
  const [totalPages, setTotalPages] = createSignal(0);
  const [stateFilter, setStateFilter] = createSignal("");

  let generation = 0;
  let listRevision = 0;
  let loadSeq = 0;
  let targetSeq = 0;
  // The page, size and filter the rendered rows answer; only a failure of that same query may keep them on screen.
  let displayedQuery = "";

  function resetAccessRequests(): void {
    generation++;
    listRevision++;
    clearListRows();
    setListError("");
    setListState("idle");
    setPage(1);
    setStateFilter("");
    clearTargets();
  }

  // Invalidate targets and fence pending reads while retaining the last list so a refused request does not erase the open picker.
  function invalidateTargets(): void {
    targetSeq++;
    setTargetsStale(true);
    // The load we just dropped owned the "loading" state, and nothing will resolve it now — leaving it set would freeze the picker on "Loading…". Fall back to what we still hold.
    if (targetState() === "loading") {
      setTargetState(targets().length > 0 ? "ready" : "idle");
    }
  }

  // clearTargets is the principal-switch discard: a new user may target an entirely different set, so the previous list must not survive at all.
  function clearTargets(): void {
    targetSeq++;
    setTargets([]);
    setTargetError("");
    setTargetState("idle");
    setTargetsStale(false);
  }

  // loadTargets fences responses by principal and sequence, reports failures as state, and preserves the last list on error. Only a successful response can establish that a target disappeared.
  async function loadTargets(): Promise<TargetLoad> {
    const gen = generation;
    const seq = ++targetSeq;
    setTargetState("loading");
    try {
      const { connections } = await requestsClient.listRequestableConnections({});
      if (gen !== generation || seq !== targetSeq) return "superseded";
      setTargets(connections);
      setTargetError("");
      setTargetState("ready");
      setTargetsStale(false);
      return "ok";
    } catch (err) {
      if (gen !== generation || seq !== targetSeq) return "superseded";
      setTargetError(errorMessage(err));
      setTargetState("error");
      return "error";
    }
  }

  async function loadAccessRequests(): Promise<void> {
    const gen = generation;
    const revision = listRevision;
    const seq = ++loadSeq;
    const query = listQueryKey(page(), pageSize(), stateFilter());
    setListState("loading");
    try {
      const res = await requestsClient.list({
        page: page(),
        pageSize: pageSize(),
        state: stateFilter(),
      });
      if (gen !== generation || seq !== loadSeq) return;
      if (revision !== listRevision) {
        void loadAccessRequests();
        return;
      }
      setAccessRequests(res.items);
      setPage(res.page || 1);
      setPageSize(res.pageSize || 20);
      setTotalCount(res.totalCount);
      setTotalPages(res.totalPages);
      displayedQuery = listQueryKey(page(), pageSize(), stateFilter());
      setListStale(false);
      setListError("");
      setListState("ready");
    } catch (err) {
      if (gen !== generation || seq !== loadSeq) return;
      if (revision !== listRevision) {
        void loadAccessRequests();
        return;
      }
      setListError(errorMessage(err));
      setListState("error");
      // A transient failure says nothing about which requests exist, so the rows of the same query stay on screen marked stale. Lost authorization, or rows that answered a different query, must not survive.
      if (isAuthorizationFailure(err) || query !== displayedQuery) {
        clearListRows();
        return;
      }
      setListStale(accessRequests().length > 0);
    }
  }

  function clearListRows(): void {
    displayedQuery = "";
    setAccessRequests([]);
    setTotalCount(0n);
    setTotalPages(0);
    setListStale(false);
  }

  // Reload the current page after mutations because state filters, ordering, and totals may change. Fence reloads against principal changes.
  async function afterMutation(gen: number): Promise<void> {
    if (gen !== generation) return;
    // Only if this principal may read the list at all. ADR-0008 lets a custom role hold requests.create without requests.list, and the requests page supports exactly that — firing a read it cannot make would leave a permanent "permission denied" banner over a page that is working fine.
    if (!mayListAccessRequests()) return;
    await loadAccessRequests();
  }

  async function createAccessRequest(
    connectionId: string,
    sql: string,
    params: TypedParam[],
    title = "",
    body = "",
  ): Promise<AccessRequest | undefined> {
    const gen = generation;
    // A refused create/submit is the signal that our picture of the targets may be stale — the connection was archived, or its policy moved — so the cache is invalidated on ANY failure rather than on a guessed set of codes: an extra refetch on the next open is cheaper than offering a dead target.
    const { request } = await withTargetInvalidation(() =>
      requestsClient.create({ connectionId, sql, params, title, body }),
    );
    await afterMutation(gen);
    return request;
  }

  async function withTargetInvalidation<T>(call: () => Promise<T>): Promise<T> {
    try {
      return await call();
    } catch (err) {
      invalidateTargets();
      throw err;
    }
  }

  async function updateDraft(
    id: string,
    expectedVersion: bigint,
    sql: string,
    params: TypedParam[],
    title = "",
    body = "",
  ): Promise<AccessRequest | undefined> {
    const gen = generation;
    // Wrapped like create and submit: submitting a SAVED draft edits it first, so THIS is the call that meets an archived connection. Leaving it out kept the cache trusted, and the recovery that re-reads the request's real state never ran (it only fires when the cache is stale).
    const { request } = await withTargetInvalidation(() =>
      requestsClient.updateDraft({ id, expectedVersion, sql, params, title, body }),
    );
    await afterMutation(gen);
    return request;
  }

  async function submitAccessRequest(id: string, expectedVersion: bigint): Promise<void> {
    const gen = generation;
    await withTargetInvalidation(() => requestsClient.submit({ id, expectedVersion }));
    await afterMutation(gen);
  }

  async function cancelAccessRequest(id: string): Promise<void> {
    const gen = generation;
    await requestsClient.cancel({ id });
    await afterMutation(gen);
  }

  async function approveAccessRequest(id: string, reason: string): Promise<void> {
    const gen = generation;
    await requestsClient.approve({ id, reason });
    await afterMutation(gen);
  }

  async function rejectAccessRequest(id: string, reason: string): Promise<void> {
    const gen = generation;
    await requestsClient.reject({ id, reason });
    await afterMutation(gen);
  }

  async function getAccessRequest(id: string) {
    return requestsClient.get({ id });
  }

  function goToPage(next: number): void {
    setPage(next);
    void loadAccessRequests();
  }

  function setFilter(state: string): void {
    setStateFilter(state);
    setPage(1);
    void loadAccessRequests();
  }

  return {
    accessRequests,
    targets,
    loadTargets,
    invalidateTargets,
    targetError,
    targetState,
    targetsStale,
    setMayListAccessRequests,
    listError,
    listState,
    listStale,
    page,
    pageSize,
    totalCount,
    totalPages,
    stateFilter,
    resetAccessRequests,
    loadAccessRequests,
    createAccessRequest,
    updateDraft,
    submitAccessRequest,
    cancelAccessRequest,
    approveAccessRequest,
    rejectAccessRequest,
    getAccessRequest,
    goToPage,
    setFilter,
  };
});

export const {
  accessRequests,
  targets,
  loadTargets,
  invalidateTargets,
  targetError,
  targetState,
  targetsStale,
  setMayListAccessRequests,
  listError,
  listState,
  listStale,
  page,
  pageSize,
  totalCount,
  totalPages,
  stateFilter,
  resetAccessRequests,
  loadAccessRequests,
  createAccessRequest,
  updateDraft,
  submitAccessRequest,
  cancelAccessRequest,
  approveAccessRequest,
  rejectAccessRequest,
  getAccessRequest,
  goToPage,
  setFilter,
} = store;
