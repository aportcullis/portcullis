import { create, toBinary } from "@bufbuild/protobuf";
import { expect, test, type Page } from "@playwright/test";

import { GetConfigResponseSchema, MeResponseSchema } from "@/gen/portcullis/v1/auth_pb";
import { AccessRequestSchema, AccessRequestState, CreateAccessRequestResponseSchema, GetAccessRequestResponseSchema, ListAccessRequestsResponseSchema, ListRequestableConnectionsResponseSchema, RequestApprovalSchema, UpdateAccessRequestDraftResponseSchema } from "@/gen/portcullis/v1/access_requests_pb";
import { fulfillConnectError, fulfillEmptyMessage } from "@e2e/rpc";

const requestId = "00000000-0000-4000-8000-000000000002";
const connectionId = "00000000-0000-4000-8000-000000000003";
const actorId = "00000000-0000-4000-8000-000000000001";
const request = create(AccessRequestSchema, {
  id: requestId, connectionId, requester: { id: actorId },
  state: AccessRequestState.DRAFT, effectiveState: AccessRequestState.DRAFT, version: 9007199254740993n,
});
const invalidMutationMessage = "The saved request response is invalid. The draft may have been saved. Check your requests before retrying.";

async function installRequestWorkspace(page: Page) {
  const config = create(GetConfigResponseSchema, { needsBootstrap: false, maxRequestTitleChars: 120, maxRequestBodyChars: 1000 });
  const principal = create(MeResponseSchema, {
    user: { id: actorId, email: "operator@example.com", status: "active" },
    permissions: ["requests.create", "requests.list", "requests.get"],
    capabilities: ["requests.create", "requests.list", "requests.get"], authorizationRevision: 7n,
  });
  await page.route("**/portcullis.v1.Auth/GetConfig", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(GetConfigResponseSchema, config)) }));
  await page.route("**/portcullis.v1.Auth/Me", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(MeResponseSchema, principal)) }));
  const targets = create(ListRequestableConnectionsResponseSchema, { connections: [{ id: connectionId, displayName: "Contract target", dbType: "postgres" }] });
  await page.route("**/portcullis.v1.AccessRequests/ListRequestableConnections", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(ListRequestableConnectionsResponseSchema, targets)) }));
  const list = create(ListAccessRequestsResponseSchema, { page: 1, pageSize: 20 });
  await page.route("**/portcullis.v1.AccessRequests/List", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(ListAccessRequestsResponseSchema, list)) }));
  const details = create(GetAccessRequestResponseSchema, { request, payload: { sql: "select 'unfinished" } });
  await page.route("**/portcullis.v1.AccessRequests/Get", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(GetAccessRequestResponseSchema, details)) }));
}

async function composeRequest(page: Page) {
  await page.goto("/requests/new");
  await page.getByLabel("Connection", { exact: true }).selectOption(connectionId);
  await page.getByLabel("Title", { exact: true }).fill("Request contract review");
  await page.getByLabel("SQL", { exact: true }).fill("select 'unfinished");
}

for (const button of ["Save draft", "Submit"]) {
  test(`a missing Create request after ${button} reports an uncertain save without submitting or replaying`, async ({ page }) => {
    await installRequestWorkspace(page);
    let creates = 0;
    let submits = 0;
    await page.route("**/portcullis.v1.AccessRequests/Create", route => { creates++; return fulfillEmptyMessage(route); });
    await page.route("**/portcullis.v1.AccessRequests/Submit", route => { submits++; return fulfillEmptyMessage(route); });
    await composeRequest(page);
    await page.getByRole("button", { name: button, exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText(invalidMutationMessage);
    await expect(page).toHaveURL(/\/requests\/new$/);
    await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Request contract review");
    expect(creates).toBe(1);
    expect(submits).toBe(0);
  });
}

for (const responseKind of ["missing", "unrelated"]) {
  test(`a ${responseKind} UpdateDraft request retains the saved draft and does not issue another Submit`, async ({ page }) => {
    await installRequestWorkspace(page);
    let creates = 0;
    let updates = 0;
    let submits = 0;
    const response = create(CreateAccessRequestResponseSchema, { request });
    await page.route("**/portcullis.v1.AccessRequests/Create", route => { creates++; return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(CreateAccessRequestResponseSchema, response)) }); });
    await page.route("**/portcullis.v1.AccessRequests/Submit", route => { submits++; return fulfillConnectError(route, "unavailable", "Submission could not be confirmed."); });
    await page.route("**/portcullis.v1.AccessRequests/UpdateDraft", route => {
      updates++;
      if (responseKind === "missing") return fulfillEmptyMessage(route);
      const unrelated = create(UpdateAccessRequestDraftResponseSchema, { request: { ...request, id: "00000000-0000-4000-8000-000000000004" } });
      return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(UpdateAccessRequestDraftResponseSchema, unrelated)) });
    });
    await composeRequest(page);
    await page.getByRole("button", { name: "Submit", exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText("Submission could not be confirmed.");
    await page.getByRole("button", { name: "Submit", exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText(invalidMutationMessage);
    await expect(page.getByLabel("Connection", { exact: true })).toBeDisabled();
    await expect(page.getByLabel("Title", { exact: true })).toHaveValue("Request contract review");
    expect(creates).toBe(1);
    expect(updates).toBe(1);
    expect(submits).toBe(1);
  });
}

test("a valid unclassified draft response opens its saved details", async ({ page }) => {
  await installRequestWorkspace(page);
  const response = create(CreateAccessRequestResponseSchema, { request });
  await page.route("**/portcullis.v1.AccessRequests/Create", route => route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(CreateAccessRequestResponseSchema, response)) }));
  await composeRequest(page);
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/requests/${requestId}$`));
  await expect(page.locator("pre")).toHaveText("select 'unfinished");
});

test("a decision without an approver fails safely and recovers after a valid read", async ({ page }) => {
  await installRequestWorkspace(page);
  let malformed = true;
  await page.route("**/portcullis.v1.AccessRequests/Get", route => {
    const approval = create(RequestApprovalSchema, { decision: "approved", reason: "private-decision-reason", valid: true });
    const details = create(GetAccessRequestResponseSchema, { request: { ...request, approvals: malformed ? [approval] : [] } });
    return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(GetAccessRequestResponseSchema, details)) });
  });
  await page.goto(`/requests/${requestId}`);
  await expect(page.getByRole("alert")).toHaveText("The access request response is invalid.");
  await expect(page.getByText("private-decision-reason")).toHaveCount(0);
  malformed = false;
  await page.reload();
  await expect(page.getByText("No decisions yet.")).toBeVisible();
});
