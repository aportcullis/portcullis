import { create, toBinary } from "@bufbuild/protobuf";
import { expect, test, type Page, type Route } from "@playwright/test";

import { MeResponseSchema, UserSchema } from "@/gen/portcullis/v1/auth_pb";
import { AccessRequestState, ListAccessRequestsResponseSchema } from "@/gen/portcullis/v1/access_requests_pb";
import { fulfillConnectError, type ConnectErrorCode } from "@e2e/rpc";

const actorID = "00000000-0000-4000-8000-000000000001";
const requestID = "00000000-0000-4000-8000-000000000002";
const connectionID = "00000000-0000-4000-8000-000000000003";

type HeldRouteState = { kind: "waiting" } | { kind: "received"; route: Route };
type ExecutionActionScenario = {
  method: "Execute" | "Cancel";
  button: "Execute" | "Stop execution";
  state: AccessRequestState;
};
const executionActionScenarios: ExecutionActionScenario[] = [
  { method: "Execute", button: "Execute", state: AccessRequestState.APPROVED },
  { method: "Cancel", button: "Stop execution", state: AccessRequestState.EXECUTING },
];
const authorizationRefusals: ConnectErrorCode[] = ["unauthenticated", "permission_denied"];

function createHeldRoute() {
  let state: HeldRouteState = { kind: "waiting" };

  const captureRoute = (route: Route): void => {
    state = { kind: "received", route };
  };

  const hasReceivedRoute = (): boolean => state.kind === "received";

  const readReceivedRoute = (): Route => {
    if (state.kind === "waiting") throw new Error("Expected an intercepted Connect call.");
    const { route } = state;
    return route;
  };

  return { captureRoute, hasReceivedRoute, readReceivedRoute };
}

async function refreshSession(page: Page): Promise<void> {
  const response = page.waitForResponse("**/portcullis.v1.Auth/Me");
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await response;
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
}

async function createRequestWorkspace(page: Page) {
  const snapshot = {
    ...create(MeResponseSchema, {
      capabilities: ["requests.list", "requests.get", "requests.execute"], authorizationRevision: 7n,
    }),
    user: create(UserSchema, { id: actorID, email: "owner@example.com", status: "active" }),
  };
  let metadataUnavailable = false;
  let requestState = AccessRequestState.APPROVED;
  let listCalls = 0;
  let metadataCalls = 0;
  await page.route("**/portcullis.v1.Auth/Me", route => {
    metadataCalls++;
    return metadataUnavailable
      ? fulfillConnectError(route, "unavailable")
      : route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(MeResponseSchema, snapshot)) });
  });
  await page.route("**/portcullis.v1.AccessRequests/List", route => {
    listCalls++;
    return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(ListAccessRequestsResponseSchema, create(ListAccessRequestsResponseSchema, {
      items: [{ id: requestID, connectionId: connectionID, title: "Row lifetime request", requester: { id: actorID }, state: requestState, effectiveState: requestState, version: 1n }],
      page: 1, pageSize: 20, totalCount: 1n, totalPages: 1,
    }))) });
  });
  const row = page.getByRole("row", { name: /Row lifetime request/ });
  return {
    snapshot,
    row,
    setUnavailable: (value: boolean) => { metadataUnavailable = value; },
    setRequestState: (value: AccessRequestState) => { requestState = value; },
    readListCallCount: () => listCalls,
    readMetadataCallCount: () => metadataCalls,
  };
}

for (const action of executionActionScenarios) {
  for (const code of authorizationRefusals) {
    test(`an earlier ${action.method} ${code} cannot refuse a new route's creation interaction`, async ({ page }) => {
      const { snapshot, row, setRequestState } = await createRequestWorkspace(page);
      snapshot.capabilities.push("requests.create");
      setRequestState(action.state);
      const { captureRoute, hasReceivedRoute, readReceivedRoute } = createHeldRoute();
      const pattern = `**/portcullis.v1.QueryExecutions/${action.method}`;
      let writes = 0;
      await page.route(pattern, route => { writes++; captureRoute(route); });
      await page.route("**/portcullis.v1.AccessRequests/ListRequestableConnections", route => route.fulfill({
        status: 200, contentType: "application/proto", body: Buffer.alloc(0),
      }));
      await page.goto("/requests");
      await row.getByRole("button", { name: action.button, exact: true }).click();
      await expect.poll(hasReceivedRoute).toBe(true);
      await page.getByRole("button", { name: "New request", exact: true }).click();
      await expect(page).toHaveURL(/\/requests\/new$/);
      await page.getByLabel("Title", { exact: true }).fill("Current request composition");
      const completed = page.waitForResponse(pattern);
      await fulfillConnectError(readReceivedRoute(), code);
      await (await completed).finished();
      await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
      await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Current request composition");
      await expect(page.getByRole("alert")).toHaveCount(0);
      expect(writes).toBe(1);
    });

    test(`a fresh same-authority Me fences an earlier ${action.method} ${code} notification`, async ({ page }) => {
      const { row, setRequestState, readMetadataCallCount } = await createRequestWorkspace(page);
      setRequestState(action.state);
      const { captureRoute, hasReceivedRoute, readReceivedRoute } = createHeldRoute();
      const pattern = `**/portcullis.v1.QueryExecutions/${action.method}`;
      let writes = 0;
      await page.route(pattern, route => { writes++; captureRoute(route); });
      await page.goto("/requests");
      await row.getByRole("button", { name: action.button, exact: true }).click();
      await expect.poll(hasReceivedRoute).toBe(true);
      await refreshSession(page);
      const callsAfterRefresh = readMetadataCallCount();
      const completed = page.waitForResponse(pattern);
      await fulfillConnectError(readReceivedRoute(), code, "Earlier execution authorization refusal.");
      await (await completed).finished();
      await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
      await expect(row.getByRole("alert")).toHaveText("Earlier execution authorization refusal.");
      expect(readMetadataCallCount()).toBe(callsAfterRefresh);
      expect(writes).toBe(1);
    });
  }
}

test("invalid requester data fails the list safely and a refreshed valid response restores owner actions", async ({ page }) => {
  const { row } = await createRequestWorkspace(page);
  let requesterMissing = true;
  await page.route("**/portcullis.v1.AccessRequests/List", route => {
    if (!requesterMissing) return route.fallback();
    const response = create(ListAccessRequestsResponseSchema, {
      items: [{ id: requestID, connectionId: connectionID, title: "Invalid requester", state: AccessRequestState.APPROVED, effectiveState: AccessRequestState.APPROVED, version: 1n }],
      page: 1, pageSize: 20, totalCount: 1n, totalPages: 1,
    });
    return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(ListAccessRequestsResponseSchema, response)) });
  });
  await page.goto("/requests");
  await expect(page.getByRole("alert")).toHaveText("The access request response is invalid.");
  await expect(page.getByRole("button", { name: "Execute", exact: true })).toHaveCount(0);
  requesterMissing = false;
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(row.getByRole("button", { name: "Execute", exact: true })).toBeEnabled();
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("a Stop completion after route departure does not reload the abandoned request list", async ({ page }) => {
  const { snapshot, row, setRequestState, readListCallCount } = await createRequestWorkspace(page);
  const { capabilities } = snapshot;
  capabilities.push("connections.list");
  setRequestState(AccessRequestState.EXECUTING);
  const { captureRoute, hasReceivedRoute, readReceivedRoute } = createHeldRoute();
  await page.route("**/portcullis.v1.QueryExecutions/Cancel", captureRoute);
  await page.route("**/portcullis.v1.Connections/List", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) }));
  await page.goto("/requests");
  await row.getByRole("button", { name: "Stop execution", exact: true }).click();
  await expect.poll(hasReceivedRoute).toBe(true);
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Connections", exact: true }).click();
  await expect(page).toHaveURL(/\/connections$/);
  const callsAfterDeparture = readListCallCount();
  const response = page.waitForResponse("**/portcullis.v1.QueryExecutions/Cancel");
  await readReceivedRoute().fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) });
  await response;
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
  expect(readListCallCount()).toBe(callsAfterDeparture);
  await expect(page.getByRole("heading", { name: "Connections", exact: true })).toBeVisible();
});

for (const change of ["revision", "identity", "capabilities", "metadata failure"]) {
  test(`a ${change} fences an earlier Stop response without replay`, async ({ page }) => {
    const { snapshot, row, setRequestState, setUnavailable } = await createRequestWorkspace(page);
    const { user } = snapshot;
    setRequestState(AccessRequestState.EXECUTING);
    const { captureRoute, hasReceivedRoute, readReceivedRoute } = createHeldRoute();
    let stopCalls = 0;
    await page.route("**/portcullis.v1.QueryExecutions/Cancel", route => {
      stopCalls++;
      captureRoute(route);
    });
    await page.goto("/requests");
    await row.getByRole("button", { name: "Stop execution", exact: true }).click();
    await expect.poll(hasReceivedRoute).toBe(true);
    if (change === "revision") snapshot.authorizationRevision++;
    if (change === "identity") user.id = "replacement-owner";
    if (change === "capabilities") snapshot.capabilities = ["requests.list"];
    if (change === "metadata failure") setUnavailable(true);
    await refreshSession(page);
    const response = page.waitForResponse("**/portcullis.v1.QueryExecutions/Cancel");
    await fulfillConnectError(readReceivedRoute(), "unavailable", "Earlier stop failure.");
    await response;
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    await expect(page.getByText("Earlier stop failure.", { exact: true })).toHaveCount(0);
    user.id = actorID;
    snapshot.capabilities = ["requests.list", "requests.get", "requests.execute"];
    setUnavailable(false);
    await refreshSession(page);
    await expect(row.getByRole("button", { name: "Stop execution", exact: true })).toBeEnabled();
    expect(stopCalls).toBe(1);
  });
}

for (const stopOutcome of ["success", "failure"]) {
  test(`the owner can stop a pending Execute and retain the stop ${stopOutcome} across refresh`, async ({ page }) => {
    const { row, setRequestState } = await createRequestWorkspace(page);
    const {
      captureRoute: captureExecutionRoute,
      hasReceivedRoute: hasReceivedExecutionRoute,
      readReceivedRoute: readExecutionRoute,
    } = createHeldRoute();
    const {
      captureRoute: captureStopRoute,
      hasReceivedRoute: hasReceivedStopRoute,
      readReceivedRoute: readStopRoute,
    } = createHeldRoute();
    let executionCalls = 0;
    let stopCalls = 0;
    await page.route("**/portcullis.v1.QueryExecutions/Execute", route => {
      executionCalls++;
      captureExecutionRoute(route);
    });
    await page.route("**/portcullis.v1.QueryExecutions/Cancel", route => {
      stopCalls++;
      captureStopRoute(route);
    });
    await page.goto("/requests");
    await row.getByRole("button", { name: "Execute", exact: true }).click();
    await expect.poll(hasReceivedExecutionRoute).toBe(true);
    setRequestState(AccessRequestState.EXECUTING);
    const listResponse = page.waitForResponse("**/portcullis.v1.AccessRequests/List");
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await listResponse;
    const stopButton = row.getByRole("button", { name: "Stop execution", exact: true });
    await expect(stopButton).toBeEnabled();
    await stopButton.click();
    await expect.poll(hasReceivedStopRoute).toBe(true);
    await expect(stopButton).toBeDisabled();
    await refreshSession(page);
    await expect(stopButton).toBeDisabled();
    const pendingStop = readStopRoute();
    if (stopOutcome === "failure") await fulfillConnectError(pendingStop, "unavailable", "Stop could not be confirmed.");
    else await pendingStop.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) });
    await expect(stopButton).toBeEnabled();
    if (stopOutcome === "failure") await expect(row.getByRole("alert")).toHaveText("Stop could not be confirmed.");
    await readExecutionRoute().fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) });
    await expect(stopButton).toBeEnabled();
    if (stopOutcome === "failure") await expect(row.getByRole("alert")).toHaveText("Stop could not be confirmed.");
    else await expect(row.getByRole("alert")).toHaveCount(0);
    expect(executionCalls).toBe(1);
    expect(stopCalls).toBe(1);
  });
}

test("a completed Execute error survives identical session and list refreshes without replay", async ({ page }) => {
  const { row } = await createRequestWorkspace(page);
  let writes = 0;
  await page.route("**/portcullis.v1.QueryExecutions/Execute", route => { writes++; return fulfillConnectError(route, "unavailable"); });
  await page.goto("/requests");
  await row.getByRole("button", { name: "Execute", exact: true }).click();
  await expect(row.getByRole("alert")).toHaveText("simulated failure");
  await refreshSession(page);
  await expect(row.getByRole("alert")).toHaveText("simulated failure");
  expect(writes).toBe(1);
});

for (const outcome of ["success", "failure"]) {
  test(`a pending Execute remains busy through identical metadata refresh and settles its ${outcome}`, async ({ page }) => {
    const { row } = await createRequestWorkspace(page);
    const { captureRoute, hasReceivedRoute, readReceivedRoute } = createHeldRoute();
    let writes = 0;
    await page.route("**/portcullis.v1.QueryExecutions/Execute", route => { writes++; captureRoute(route); });
    await page.goto("/requests");
    await row.getByRole("button", { name: "Execute", exact: true }).click();
    await expect.poll(hasReceivedRoute).toBe(true);
    await refreshSession(page);
    await expect(row.getByRole("button", { name: "Execute", exact: true })).toBeDisabled();
    const pending = readReceivedRoute();
    if (outcome === "success") await pending.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) });
    else await fulfillConnectError(pending, "unavailable");
    await expect(row.getByRole("button", { name: "Execute", exact: true })).toBeEnabled();
    if (outcome === "failure") await expect(row.getByRole("alert")).toHaveText("simulated failure");
    else await expect(row.getByRole("alert")).toHaveCount(0);
    expect(writes).toBe(1);
  });
}

for (const change of ["revision", "identity", "capabilities", "metadata failure"]) {
  test(`a ${change} discards the old row outcome and fences its delayed completion`, async ({ page }) => {
    const { snapshot, row, setUnavailable } = await createRequestWorkspace(page);
    const { user } = snapshot;
    const { captureRoute, hasReceivedRoute, readReceivedRoute } = createHeldRoute();
    let writes = 0;
    await page.route("**/portcullis.v1.QueryExecutions/Execute", route => {
      writes++;
      if (writes === 1) return fulfillConnectError(route, "unavailable");
      captureRoute(route);
    });
    await page.goto("/requests");
    await row.getByRole("button", { name: "Execute", exact: true }).click();
    await expect(row.getByRole("alert")).toHaveText("simulated failure");
    await row.getByRole("button", { name: "Execute", exact: true }).click();
    await expect.poll(hasReceivedRoute).toBe(true);
    if (change === "revision") snapshot.authorizationRevision++;
    if (change === "identity") user.id = "replacement-owner";
    if (change === "capabilities") snapshot.capabilities = ["requests.list"];
    if (change === "metadata failure") setUnavailable(true);
    await refreshSession(page);
    const response = page.waitForResponse("**/portcullis.v1.QueryExecutions/Execute");
    await fulfillConnectError(readReceivedRoute(), "unavailable");
    await response;
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
    await expect(page.getByText("simulated failure", { exact: true })).toHaveCount(0);
    user.id = actorID;
    snapshot.capabilities = ["requests.list", "requests.get", "requests.execute"];
    setUnavailable(false);
    await refreshSession(page);
    await expect(row).toBeVisible();
    await expect(row.getByRole("alert")).toHaveCount(0);
    await expect(row.getByRole("button", { name: "Execute", exact: true })).toBeEnabled();
    expect(writes).toBe(2);
  });
}
