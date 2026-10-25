import { create, toBinary } from "@bufbuild/protobuf";
import { expect, test, type Page, type Route } from "@playwright/test";

import { CreateRoleResponseSchema } from "@/gen/portcullis/v1/administration_pb";
import { GetConfigResponseSchema, MeResponseSchema } from "@/gen/portcullis/v1/auth_pb";
import { fulfillConnectError } from "@e2e/rpc";

type HeldRouteState = { kind: "waiting" } | { kind: "received"; route: Route };
type SessionReplyKind = "authenticated" | "anonymous" | "unavailable";

function holdBackgroundRead() {
  let state: HeldRouteState = { kind: "waiting" };
  return {
    receive(route: Route): void { state = { kind: "received", route }; },
    received(): boolean { return state.kind === "received"; },
    async refuse(code: "permission_denied" | "unauthenticated"): Promise<void> {
      if (state.kind !== "received") throw new Error("The background read has not arrived");
      await fulfillConnectError(state.route, code);
    },
  };
}

async function installCurrentAuthority(page: Page) {
  const config = create(GetConfigResponseSchema, { needsBootstrap: false });
  await page.route("**/portcullis.v1.Auth/GetConfig", route => route.fulfill({
    status: 200, contentType: "application/proto", body: Buffer.from(toBinary(GetConfigResponseSchema, config)),
  }));
  const snapshot = create(MeResponseSchema, {
    user: { id: "00000000-0000-4000-8000-000000000001", email: "operator@example.com", status: "active" },
    permissions: ["requests.list", "requests.approve", "roles.create", "roles.list"],
    capabilities: ["requests.list", "requests.approve", "roles.create", "roles.list"],
    authorizationRevision: 7n,
  });
  let reply: SessionReplyKind = "authenticated";
  await page.route("**/portcullis.v1.Auth/Me", route => {
    if (reply === "unavailable") return fulfillConnectError(route, "unavailable");
    const message = reply === "authenticated" ? snapshot : create(MeResponseSchema);
    return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.from(toBinary(MeResponseSchema, message)) });
  });
  return {
    snapshot,
    endSession() { reply = "anonymous"; },
    failMetadata() { reply = "unavailable"; },
    restoreMetadata() { reply = "authenticated"; },
  };
}

async function createRoleReceipt(page: Page) {
  const authority = await installCurrentAuthority(page);
  const held = holdBackgroundRead();
  let reads = 0;
  let writes = 0;
  await page.route("**/portcullis.v1.AccessRequests/List", route => {
    reads++;
    if (reads === 1) { held.receive(route); return; }
    return route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) });
  });
  await page.route("**/portcullis.v1.Roles/Create", route => {
    writes++;
    return route.fulfill({
      status: 200,
      contentType: "application/proto",
      body: Buffer.from(toBinary(CreateRoleResponseSchema, create(CreateRoleResponseSchema, {
        role: { id: "00000000-0000-4000-8000-000000000002", version: 1n },
      }))),
    });
  });
  await page.goto("/roles/new");
  await expect.poll(held.received).toBe(true);
  await page.getByLabel("Role name").fill("New reader");
  await page.getByLabel("Permission keys").fill("requests.get");
  await page.getByRole("button", { name: "Create role", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Created role");
  return { authority, held, reads: () => reads, writes: () => writes };
}

test("a refused background pending count rechecks authority without discarding an unrelated created Role receipt", async ({ page }) => {
  const { held, reads, writes } = await createRoleReceipt(page);
  const rechecked = page.waitForResponse("**/portcullis.v1.Auth/Me");
  await held.refuse("permission_denied");
  await rechecked;
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
  await expect(page.getByRole("status")).toContainText("Created role");
  await expect(page.getByLabel("Role ID receipt")).toHaveValue("00000000-0000-4000-8000-000000000002");
  expect(reads()).toBe(1);
  expect(writes()).toBe(1);
});

test("session loss from the background read still discards current creation delivery", async ({ page }) => {
  const { authority, held, writes } = await createRoleReceipt(page);
  authority.endSession();
  await held.refuse("unauthenticated");
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  await expect(page.getByLabel("Role ID receipt")).toHaveCount(0);
  expect(writes()).toBe(1);
});

test("a fresh same-authority Me fences an older background session refusal", async ({ page }) => {
  const { held, writes } = await createRoleReceipt(page);
  const rechecked = page.waitForResponse("**/portcullis.v1.Auth/Me");
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await rechecked;
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
  const completed = page.waitForResponse("**/portcullis.v1.AccessRequests/List");
  await held.refuse("unauthenticated");
  await (await completed).finished();
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
  await expect(page.getByLabel("Role ID receipt")).toHaveValue("00000000-0000-4000-8000-000000000002");
  await expect(page.getByRole("alert")).toHaveCount(0);
  expect(writes()).toBe(1);
});

test("a background refusal with confirmed changed authority discards the creation receipt", async ({ page }) => {
  const { authority, held, writes } = await createRoleReceipt(page);
  authority.snapshot.capabilities = authority.snapshot.capabilities.filter(permission => permission !== "roles.create");
  await held.refuse("permission_denied");
  await expect(page.getByText(/requires separate roles.create/)).toBeVisible();
  await expect(page.getByLabel("Role ID receipt")).toHaveCount(0);
  expect(writes()).toBe(1);
});

test("failed authority re-resolution discards the receipt and recovery does not resurrect it", async ({ page }) => {
  const { authority, held, writes } = await createRoleReceipt(page);
  authority.failMetadata();
  await held.refuse("permission_denied");
  await expect(page.getByRole("heading", { name: "Portcullis is temporarily unreachable" })).toBeVisible();
  await expect(page.getByLabel("Role ID receipt")).toHaveCount(0);
  authority.restoreMetadata();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByLabel("Role name")).toHaveValue("");
  await expect(page.getByLabel("Role ID receipt")).toHaveCount(0);
  expect(writes()).toBe(1);
});

test("a direct protected catalog refusal still discards a creation receipt", async ({ page }) => {
  const { held, writes } = await createRoleReceipt(page);
  const rechecked = page.waitForResponse("**/portcullis.v1.Auth/Me");
  await held.refuse("permission_denied");
  await rechecked;
  await expect(page.getByLabel("Role ID receipt")).toHaveValue("00000000-0000-4000-8000-000000000002");
  await page.route("**/portcullis.v1.Roles/ListPermissions", route => fulfillConnectError(route, "permission_denied"));
  await page.getByRole("button", { name: "Load permission catalog", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Permission changed");
  await expect(page.getByLabel("Role ID receipt")).toHaveCount(0);
  expect(writes()).toBe(1);
});
