import { create, toBinary } from "@bufbuild/protobuf";
import { expect, test, type Page, type Route } from "@playwright/test";

import { MeResponseSchema } from "@/gen/portcullis/v1/auth_pb";
import { AccessRequestState, ListAccessRequestsResponseSchema } from "@/gen/portcullis/v1/access_requests_pb";
import { fulfillConnectError } from "@e2e/rpc";

const actorID = "00000000-0000-4000-8000-000000000001";
const requestID = "00000000-0000-4000-8000-000000000002";

/** Refreshes session metadata through the mounted coordinator before checking row state. */
async function refreshSession(page: Page): Promise<void> {
  const response = page.waitForResponse("**/portcullis.v1.Auth/Me");
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await response;
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
}

/** Supplies a permitted owner and replaceable list rows to the real SPA. */
async function requestWorkspace(page: Page) {
  const snapshot = create(MeResponseSchema, {
    user: { id: actorID, email: "owner@example.com", status: "active" },
    capabilities: ["requests.list", "requests.get", "requests.execute"], authorizationRevision: 7n,
  });
  let metadataUnavailable = false;
  await page.route("**/portcullis.v1.Auth/Me", route => metadataUnavailable
    ? fulfillConnectError(route, "unavailable")
    : route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(MeResponseSchema, snapshot)) }));
  await page.route("**/portcullis.v1.AccessRequests/List", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(ListAccessRequestsResponseSchema, create(ListAccessRequestsResponseSchema, {
    items: [{ id: requestID, title: "Row lifetime request", requester: { id: actorID }, state: AccessRequestState.APPROVED, effectiveState: AccessRequestState.APPROVED, version: 1n }],
    page: 1, pageSize: 20, totalCount: 1n, totalPages: 1,
  }))) }));
  const row = page.getByRole("row", { name: /Row lifetime request/ });
  return { snapshot, row, setUnavailable: (value: boolean) => { metadataUnavailable = value; } };
}

test("a completed Execute error survives identical session and list refreshes without replay", async ({ page }) => {
  const workspace = await requestWorkspace(page);
  let writes = 0;
  await page.route("**/portcullis.v1.QueryExecutions/Execute", route => { writes++; return fulfillConnectError(route, "unavailable"); });
  await page.goto("/requests");
  await workspace.row.getByRole("button", { name: "Execute", exact: true }).click();
  await expect(workspace.row.getByRole("alert")).toHaveText("simulated failure");
  await refreshSession(page);
  await expect(workspace.row.getByRole("alert")).toHaveText("simulated failure");
  expect(writes).toBe(1);
});

for (const outcome of ["success", "failure"]) {
  test(`a pending Execute remains busy through identical metadata refresh and settles its ${outcome}`, async ({ page }) => {
    const workspace = await requestWorkspace(page);
    let pending: Route | undefined;
    let writes = 0;
    await page.route("**/portcullis.v1.QueryExecutions/Execute", route => { writes++; pending = route; });
    await page.goto("/requests");
    await workspace.row.getByRole("button", { name: "Execute", exact: true }).click();
    await expect.poll(() => pending !== undefined).toBe(true);
    await refreshSession(page);
    await expect(workspace.row.getByRole("button", { name: "Execute", exact: true })).toBeDisabled();
    if (!pending) throw new Error("Expected the pending execution request");
    if (outcome === "success") await pending.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) });
    else await fulfillConnectError(pending, "unavailable");
    await expect(workspace.row.getByRole("button", { name: "Execute", exact: true })).toBeEnabled();
    if (outcome === "failure") await expect(workspace.row.getByRole("alert")).toHaveText("simulated failure");
    else await expect(workspace.row.getByRole("alert")).toHaveCount(0);
    expect(writes).toBe(1);
  });
}

for (const change of ["revision", "identity", "capabilities", "metadata failure"]) {
  test(`a ${change} discards the old row outcome and fences its delayed completion`, async ({ page }) => {
    const workspace = await requestWorkspace(page);
    let pending: Route | undefined;
    let writes = 0;
    await page.route("**/portcullis.v1.QueryExecutions/Execute", route => {
      writes++;
      if (writes === 1) return fulfillConnectError(route, "unavailable");
      pending = route;
    });
    await page.goto("/requests");
    await workspace.row.getByRole("button", { name: "Execute", exact: true }).click();
    await expect(workspace.row.getByRole("alert")).toHaveText("simulated failure");
    await workspace.row.getByRole("button", { name: "Execute", exact: true }).click();
    await expect.poll(() => pending !== undefined).toBe(true);
    if (change === "revision") workspace.snapshot.authorizationRevision++;
    if (change === "identity" && workspace.snapshot.user) workspace.snapshot.user.id = "replacement-owner";
    if (change === "capabilities") workspace.snapshot.capabilities = ["requests.list"];
    if (change === "metadata failure") workspace.setUnavailable(true);
    await refreshSession(page);
    const response = page.waitForResponse("**/portcullis.v1.QueryExecutions/Execute");
    if (!pending) throw new Error("Expected the pending execution request");
    await fulfillConnectError(pending, "unavailable");
    await response;
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
    await expect(page.getByText("simulated failure", { exact: true })).toHaveCount(0);
    if (workspace.snapshot.user) workspace.snapshot.user.id = actorID;
    workspace.snapshot.capabilities = ["requests.list", "requests.get", "requests.execute"];
    workspace.setUnavailable(false);
    await refreshSession(page);
    await expect(workspace.row).toBeVisible();
    await expect(workspace.row.getByRole("alert")).toHaveCount(0);
    await expect(workspace.row.getByRole("button", { name: "Execute", exact: true })).toBeEnabled();
    expect(writes).toBe(2);
  });
}
