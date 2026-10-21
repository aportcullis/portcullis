import { create, toBinary } from "@bufbuild/protobuf";
import { expect, test, type Locator, type Page, type Route } from "@playwright/test";

import { GetUserResponseSchema } from "@/gen/portcullis/v1/administration_pb";
import { MeResponseSchema } from "@/gen/portcullis/v1/auth_pb";
import { fulfillConnectError } from "@e2e/rpc";

/** Measures actual browser colors, compositing transparent surfaces and hover fills into sRGB. */
async function measureControlContrast(
  locator: Locator,
  property: "color" | "borderTopColor" | "outlineColor" = "color",
): Promise<number> {
  return locator.evaluate((element, selectedProperty) => {
    const canvas = document.createElement("canvas");
    canvas.width = 1;
    canvas.height = 1;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    if (!context) throw new Error("Canvas colors are unavailable");
    const ancestors: Element[] = [];
    for (let ancestor: Element | null = element; ancestor; ancestor = ancestor.parentElement)
      ancestors.unshift(ancestor);
    context.fillStyle = "white";
    context.fillRect(0, 0, 1, 1);
    for (const ancestor of ancestors) {
      context.fillStyle = getComputedStyle(ancestor).backgroundColor;
      context.fillRect(0, 0, 1, 1);
    }
    const background = context.getImageData(0, 0, 1, 1).data;
    context.fillStyle = getComputedStyle(element)[selectedProperty];
    context.fillRect(0, 0, 1, 1);
    const foreground = context.getImageData(0, 0, 1, 1).data;
    const luminance = (channels: Uint8ClampedArray) => {
      const linear = Array.from(channels)
        .slice(0, 3)
        .map((channel) => {
          const normalized = channel / 255;
          return normalized <= 0.04045 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4;
        });
      return (linear[0] ?? 0) * 0.2126 + (linear[1] ?? 0) * 0.7152 + (linear[2] ?? 0) * 0.0722;
    };
    const first = luminance(foreground),
      second = luminance(background);
    return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05);
  }, property);
}

/** Waits for the actual hover transition before measuring its final colors. */
async function hoverControl(locator: Locator): Promise<void> {
  await locator.hover();
  await locator.evaluate(async (element) => {
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await Promise.all(element.getAnimations().map((animation) => animation.finished));
  });
}

/** Serves advisory metadata while the real application owns controls, routing and lifecycle fences. */
async function mockWorkspace(page: Page) {
  const snapshot = create(MeResponseSchema, {
    user: {
      id: "00000000-0000-4000-8000-000000000001",
      email: "operator@example.com",
      status: "active",
    },
    capabilities: ["users.create", "bindings.create", "requests.create"],
    authorizationRevision: 7n,
  });
  await page.route("**/portcullis.v1.Auth/Me", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/proto",
      body: Buffer.from(toBinary(MeResponseSchema, snapshot)),
    }),
  );
  await page.route("**/portcullis.v1.Auth/GetConfig", (route) =>
    route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) }),
  );
  await page.route("**/portcullis.v1.AccessRequests/ListRequestableConnections", (route) =>
    route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) }),
  );
  return snapshot;
}

/** Applies an existing CSS theme without adding a product theme preference. */
async function openThemedPage(page: Page, path: string, dark: boolean): Promise<void> {
  await page.goto(path);
  await page.evaluate((value) => document.documentElement.classList.toggle("dark", value), dark);
}

/** Waits for the session coordinator and reactive disposal after a metadata refresh. */
async function refreshSession(page: Page): Promise<void> {
  const response = page.waitForResponse("**/portcullis.v1.Auth/Me");
  await page.evaluate(() => window.dispatchEvent(new Event("online")));
  await response;
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())));
}

for (const dark of [false, true]) {
  const theme = dark ? "dark" : "light";
  test(`${theme}: default fields match selects with readable text, boundaries and keyboard focus`, async ({
    page,
  }) => {
    const snapshot = await mockWorkspace(page);
    await openThemedPage(page, "/bindings/new", dark);
    const field = page.getByLabel("User ID", { exact: true });
    const select = page.getByLabel("Scope", { exact: true });
    await field.fill("Locally composed input");
    for (const control of [field, select]) {
      await expect(control).toHaveCSS("height", "40px");
      expect(await measureControlContrast(control)).toBeGreaterThanOrEqual(4.5);
      expect(await measureControlContrast(control, "borderTopColor")).toBeGreaterThanOrEqual(3);
    }
    await field.focus();
    await page.keyboard.press("Tab");
    await expect(page.getByLabel("Role ID", { exact: true })).toBeFocused();
    const focused = page.getByLabel("Role ID", { exact: true });
    await expect(focused).toHaveCSS("outline-style", "solid");
    await expect(focused).toHaveCSS("outline-width", "2px");
    expect(await measureControlContrast(focused, "outlineColor")).toBeGreaterThanOrEqual(3);
    snapshot.authorizationRevision++;
    await refreshSession(page);
    await expect(field).toHaveValue("Locally composed input");
    await page.screenshot({ path: test.info().outputPath(`binding-${theme}.png`), fullPage: true });
  });

  test(`${theme}: compact parameter controls align and NULL stays disabled without writes`, async ({
    page,
  }) => {
    await mockWorkspace(page);
    await openThemedPage(page, "/requests/new", dark);
    await page.getByRole("button", { name: "Add parameter" }).click();
    const name = page.getByLabel("Name", { exact: true });
    const type = page.getByLabel("Type", { exact: true });
    const value = page.getByLabel("Value", { exact: true });
    const remove = page.getByRole("button", { name: "Remove", exact: true });
    for (const control of [name, type, value, remove])
      await expect(control).toHaveCSS("height", "36px");
    await name.fill("customer_id");
    await value.fill("42");
    await type.selectOption("null");
    await expect(value).toBeDisabled();
    await type.selectOption("integer");
    await expect(value).toBeEnabled();
    await expect(name).toHaveValue("customer_id");
    await page.setViewportSize({ width: 320, height: 800 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  });

  test(`${theme}: primary rest and hover remain readable and pending writes stay disabled across refresh`, async ({
    page,
  }) => {
    await mockWorkspace(page);
    let pending: Route | undefined;
    let writes = 0;
    await page.route("**/portcullis.v1.Users/Create", (route) => {
      writes++;
      pending = route;
    });
    await openThemedPage(page, "/users/new", dark);
    await page.getByLabel("Email", { exact: true }).fill("member@example.com");
    await page.getByLabel("Display name").fill("Member");
    const button = page.getByRole("button", { name: "Create user", exact: true });
    expect(await measureControlContrast(button)).toBeGreaterThanOrEqual(4.5);
    await hoverControl(button);
    expect(await measureControlContrast(button)).toBeGreaterThanOrEqual(4.5);
    await button.click();
    await expect.poll(() => pending !== undefined).toBe(true);
    await expect(button).toBeDisabled();
    await refreshSession(page);
    await expect(button).toBeDisabled();
    expect(writes).toBe(1);
    if (!pending) throw new Error("Expected the pending user creation");
    await fulfillConnectError(pending, "unavailable");
    await expect(page.getByRole("alert")).toContainText("Reload to check");
    await expect(button).toBeDisabled();
    expect(writes).toBe(1);
  });

  test(`${theme}: dangerous confirmation stays readable and cancelling never changes account state`, async ({
    page,
  }) => {
    const snapshot = await mockWorkspace(page);
    snapshot.capabilities.push("users.get", "users.disable");
    const memberID = "00000000-0000-4000-8000-000000000002";
    let writes = 0;
    page.on("request", (request) => {
      if (request.url().endsWith("/portcullis.v1.Users/Disable")) writes++;
    });
    await page.route("**/portcullis.v1.Users/Get", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/proto",
        body: Buffer.from(
          toBinary(
            GetUserResponseSchema,
            create(GetUserResponseSchema, {
              user: {
                user: { id: memberID, email: "member@example.com", status: "active" },
                hasPassword: true,
              },
            }),
          ),
        ),
      }),
    );
    await openThemedPage(page, `/users/${memberID}`, dark);
    const trigger = page.getByRole("button", { name: "Disable user", exact: true });
    await trigger.click();
    const dialog = page.getByRole("dialog");
    const confirm = dialog.getByRole("button", { name: "Confirm", exact: true });
    expect(await measureControlContrast(confirm)).toBeGreaterThanOrEqual(4.5);
    await hoverControl(confirm);
    expect(await measureControlContrast(confirm)).toBeGreaterThanOrEqual(4.5);
    await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(trigger).toBeFocused();
    expect(writes).toBe(0);
  });

  test(`${theme}: invalid Binding feedback is readable and sends no grant`, async ({ page }) => {
    await mockWorkspace(page);
    let writes = 0;
    page.on("request", (request) => {
      if (request.url().endsWith("/portcullis.v1.Bindings/Create")) writes++;
    });
    await openThemedPage(page, "/bindings/new", dark);
    await page.getByLabel("User ID", { exact: true }).fill("invalid");
    await page.getByLabel("Role ID", { exact: true }).fill("invalid");
    await page.getByRole("button", { name: "Create binding", exact: true }).click();
    const alert = page.getByRole("alert");
    await expect(alert).toContainText("canonical UUIDs");
    expect(await measureControlContrast(alert)).toBeGreaterThanOrEqual(4.5);
    expect(writes).toBe(0);
  });

  test(`${theme}: refused sign-in stays uniform with readable destructive feedback at 320 pixels`, async ({
    page,
  }) => {
    await mockWorkspace(page);
    await page.route("**/portcullis.v1.Auth/Me", (route) =>
      route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) }),
    );
    await page.route("**/portcullis.v1.Auth/Login", (route) =>
      fulfillConnectError(route, "unauthenticated"),
    );
    await page.setViewportSize({ width: 320, height: 800 });
    await openThemedPage(page, "/login", dark);
    await page.getByLabel("Email", { exact: true }).fill("member@example.com");
    await page.getByLabel("Password", { exact: true }).fill("wrong-password");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    const alert = page.getByRole("alert");
    await expect(alert).toContainText("couldn't sign you in");
    expect(await measureControlContrast(alert)).toBeGreaterThanOrEqual(4.5);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  });

  test(`${theme}: authority loss discards inputs and a delayed reply cannot restore them`, async ({
    page,
  }) => {
    const snapshot = await mockWorkspace(page);
    let pending: Route | undefined;
    await page.route("**/portcullis.v1.Users/Create", (route) => {
      pending = route;
    });
    await openThemedPage(page, "/users/new", dark);
    await page.getByLabel("Email", { exact: true }).fill("discard@example.com");
    await page.getByLabel("Display name").fill("Member");
    await page.getByRole("button", { name: "Create user", exact: true }).click();
    await expect.poll(() => pending !== undefined).toBe(true);
    snapshot.capabilities = ["requests.create"];
    await refreshSession(page);
    await expect(page.getByLabel("Email", { exact: true })).toHaveCount(0);
    if (!pending) throw new Error("Expected the pending creation");
    await fulfillConnectError(pending, "unavailable");
    snapshot.capabilities.push("users.create");
    await refreshSession(page);
    await page.goto("/users/new");
    await expect(page.getByLabel("Email", { exact: true })).toHaveValue("");
    await expect(page.getByText("simulated failure", { exact: true })).toHaveCount(0);
  });
}
