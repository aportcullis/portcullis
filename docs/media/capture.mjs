import { spawn, execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { fileURLToPath } from "node:url";
import { chromium, expect } from "../../web/node_modules/@playwright/test/index.mjs";

// Capture the real embedded application against an owned, disposable demo database.
const root = fileURLToPath(new URL("../../", import.meta.url));
const media = fileURLToPath(new URL("./", import.meta.url));
const work = root + ".test-docker/readme-media/";
const baseURL = "http://127.0.0.1:18080";
const password = "readme-demo-password-only";
const image = "postgres:18.4-alpine3.24@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15";
const run = (command, args) => execFileSync(command, args, { cwd: root, encoding: "utf8", stdio: ["pipe", "pipe", "inherit"] }).trim();
const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
// Refuse occupied ports before starting anything; never capture or seed another installation.
for (const port of [15432, 18080]) {
  await new Promise((resolve, reject) => {
    const probe = createServer();
    probe.once("error", reject);
    probe.listen(port, "127.0.0.1", () => probe.close(resolve));
  });
}
await mkdir(work, { recursive: true });
run("pnpm", ["-C", "web", "build"]);
run("go", ["build", "-o", work + "portcullis", "./cmd/portcullis"]);
const cid = run("docker", ["run", "--detach", "--rm", "--label", "portcullis.demo=readme", "-e", "POSTGRES_USER=portcullis", "-e", "POSTGRES_PASSWORD=portcullis", "-e", "POSTGRES_DB=portcullis", "-p", "127.0.0.1:15432:5432", image]);
let server;
let browser;
const frames = { workflow: [], results: [] };
try {
  let ready = false;
  for (let i = 0; i < 60; i++) {
    try { run("docker", ["exec", cid, "pg_isready", "-U", "portcullis"]); ready = true; break; }
    catch { await wait(1000); }
  }
  if (!ready) throw new Error("Demo database did not become ready");
  server = spawn(work + "portcullis", [], { cwd: root, env: {
    ...Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("PORTCULLIS_"))),
    PORTCULLIS_DATABASE_URL: "postgres://portcullis:portcullis@127.0.0.1:15432/portcullis?sslmode=disable",
    PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME: "true", PORTCULLIS_STARTUP_MIGRATE: "true",
    PORTCULLIS_MASTER_KEY: randomBytes(32).toString("base64"), PORTCULLIS_ADDR: "127.0.0.1:18080",
  }, stdio: ["ignore", "ignore", "inherit"] });
  ready = false;
  for (let i = 0; i < 90; i++) {
    if (server.exitCode !== null) throw new Error("Demo server exited before readiness");
    try { if ((await fetch(baseURL + "/readyz")).ok) { ready = true; break; } } catch { /* Wait for migrations. */ }
    await wait(1000);
  }
  if (!ready) throw new Error("Demo server did not become ready");
  browser = await chromium.launch();
  const contextOptions = { baseURL, viewport: { width: 1280, height: 1000 }, deviceScaleFactor: 1, colorScheme: "light", reducedMotion: "reduce" };
  const context = await browser.newContext(contextOptions);
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  const shot = async (name, target = page) => {
    await target.evaluate(() => document.fonts.ready);
    for (const image of await target.locator("img").all()) {
      await expect.poll(() => image.evaluate(element => element.complete && element.naturalWidth > 0)).toBeTruthy();
    }
    await target.screenshot({ path: media + name + ".png", animations: "disabled", caret: "hide" });
  };
  const frame = async (group, caption, duration = 1600, target = page) => {
    await target.evaluate(() => document.fonts.ready);
    const path = work + group + "-" + String(frames[group].length).padStart(3, "0") + ".png";
    await target.screenshot({ path, animations: "disabled", caret: "hide" });
    frames[group].push({ path, caption, duration });
  };
  const login = async (target, email) => {
    await target.goto("/login");
    await target.getByLabel("Email").fill(email);
    await target.getByLabel("Password", { exact: true }).fill(password);
    await target.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(target.getByRole("button", { name: "Sign out" })).toBeVisible();
  };
  await page.goto("/");
  await expect(page).toHaveURL(/\/bootstrap$/);
  await expect(page.getByRole("img", { name: "Portcullis" })).toBeVisible();
  await shot("bootstrap");
  await page.getByLabel("Email").fill("alex@example.test");
  await page.getByLabel("Display name").fill("Alex · Platform");
  await page.getByLabel(/^Password/).fill(password);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("img", { name: "Portcullis" })).toBeVisible();
  await shot("login");
  // M1 has no provisioning UI: add a second, distinct reviewer only in this disposable fixture.
  const seed = `BEGIN;
    INSERT INTO users(email, display_name) VALUES ('sam@example.test', 'Sam · Reviewer');
    INSERT INTO auth_methods(user_id, type, secret)
      SELECT reviewer.id, 'password', auth.secret FROM users reviewer
      CROSS JOIN users original JOIN auth_methods auth ON auth.user_id = original.id AND auth.type = 'password'
      WHERE reviewer.email = 'sam@example.test' AND original.email = 'alex@example.test';
    INSERT INTO organization_memberships(organization_id, user_id, role_id)
      SELECT roles.organization_id, users.id, roles.id FROM users CROSS JOIN roles
      JOIN organizations ON organizations.id = roles.organization_id
      WHERE users.email = 'sam@example.test' AND roles.name = 'approver' AND organizations.slug = 'default';
    COMMIT;`;
  execFileSync("docker", ["exec", "-i", cid, "psql", "-v", "ON_ERROR_STOP=1", "-U", "portcullis", "-d", "portcullis"], { input: seed, stdio: ["pipe", "ignore", "inherit"] });
  await login(page, "alex@example.test");
  for (const [name, environment] of [["Analytics warehouse", "development"], ["Orders replica", "production"]]) {
    await page.getByRole("button", { name: "New connection" }).click();
    await page.getByLabel("Display name").fill(name);
    await page.getByLabel("Environment").selectOption(environment);
    await page.getByLabel("Host").fill("127.0.0.1");
    await page.getByLabel("Port").fill("15432");
    await page.getByLabel("Database", { exact: true }).fill("portcullis");
    await page.getByLabel("User", { exact: true }).fill("portcullis");
    await page.getByLabel("Password", { exact: true }).fill("portcullis");
    await page.getByLabel("TLS mode").selectOption("disable");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("row", { name: new RegExp(name) })).toBeVisible();
  }
  await shot("connections");
  await page.getByRole("row", { name: /Analytics warehouse/ }).getByRole("button", { name: "Policy" }).click();
  await expect(page.getByLabel("Read approvals")).toHaveValue("1");
  await shot("policy");
  await page.getByRole("button", { name: "Dismiss" }).click();
  await page.getByRole("link", { name: /Requests/ }).click();
  await expect(page.getByRole("button", { name: "New request" })).toBeVisible();
  await expect(page.getByText("No access requests yet.", { exact: true })).toBeVisible();
  await frame("workflow", "1 / 6  ·  Requester: create a SQL access request");
  await page.getByRole("button", { name: "New request" }).click();
  await page.getByLabel("Connection").selectOption({ label: "Analytics warehouse" });
  const sql = `SELECT (9007199254740993::bigint + g) AS order_id,
       'Demo account ' || g::text AS account,
       (ARRAY['APAC', 'EMEA', 'AMER'])[1 + ((g - 1) % 3)] AS region,
       (g * 19.95)::numeric(12,2) AS revenue_usd
FROM generate_series(1, 30) AS g`;
  await page.getByLabel("SQL", { exact: true }).fill(sql);
  await expect(page).toHaveURL(/\/requests\/new$/);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await shot("request");
  await frame("workflow", "2 / 6  ·  Choose the connection and submit the exact SQL", 2400);
  await page.getByRole("button", { name: "Submit", exact: true }).click();
  const requestRow = target => target.getByRole("row", { name: /Analytics warehouse/ });
  const requestDetail = target => target.getByRole("region", { name: "Request details" });
  await expect(requestDetail(page).getByText("Pending", { exact: true })).toBeVisible();
  await frame("workflow", "3 / 6  ·  Pending: execution waits for a distinct reviewer", 2400);
  const reviewerContext = await browser.newContext(contextOptions);
  const reviewer = await reviewerContext.newPage();
  await login(reviewer, "sam@example.test");
  await reviewer.goto("/requests");
  await requestRow(reviewer).getByRole("link", { name: "Details" }).click();
  await expect(reviewer.getByRole("button", { name: "Approve", exact: true })).toBeVisible();
  await reviewer.getByLabel("Reason (required to reject)").fill("Read-only synthetic data; approved for analysis.");
  const reviewBounds = await requestDetail(reviewer).boundingBox();
  const approveBounds = await reviewer.getByRole("button", { name: "Approve", exact: true }).boundingBox();
  expect(approveBounds.x + approveBounds.width, "Long SQL must not push the approval action outside the page").toBeLessThanOrEqual(reviewBounds.x + reviewBounds.width);
  await shot("review", reviewer);
  await frame("workflow", "4 / 6  ·  Reviewer: inspect the frozen SQL and approve", 2800, reviewer);
  await reviewer.getByRole("button", { name: "Approve", exact: true }).click();
  await expect(requestDetail(reviewer).getByText("Approved", { exact: true })).toBeVisible();
  await page.reload();
  await expect(requestDetail(page).getByText("Approved", { exact: true })).toBeVisible();
  await frame("workflow", "5 / 6  ·  Requester: run the approved statement once", 2200);
  await requestDetail(page).getByRole("button", { name: "Execute", exact: true }).click();
  await expect(requestDetail(page).getByText("Succeeded", { exact: true })).toBeVisible();
  await expect(requestDetail(page).getByRole("button", { name: "Execute", exact: true })).toHaveCount(0);
  await frame("workflow", "6 / 6  ·  Succeeded: inspect the result; execution cannot replay", 2600);
  await requestDetail(page).getByRole("link", { name: "Result", exact: true }).click();
  const dialog = page.getByRole("region", { name: "Query results" });
  await expect(dialog.getByText("9007199254740994", { exact: true })).toBeVisible();
  await dialog.getByLabel("Rows per page").selectOption("10");
  await expect(dialog.getByText(/30 rows · Page 1 of 3/)).toBeVisible();
  await shot("results");
  await frame("results", "1 / 5  ·  Explore exact integers, decimals and typed columns", 2600);
  await dialog.getByRole("button", { name: "Next page", exact: true }).click();
  await expect(dialog.getByText(/Page 2 of 3/)).toBeVisible();
  await frame("results", "2 / 5  ·  Browse the snapshot with server-side pagination", 1800);
  await dialog.getByRole("button", { name: /revenue_usd/ }).click();
  await expect(dialog.getByText(/Page 1 of 3/)).toBeVisible();
  await dialog.getByRole("button", { name: /revenue_usd/ }).click();
  await expect(dialog.getByRole("row").nth(1)).toContainText("598.50");
  await frame("results", "3 / 5  ·  Sort revenue numerically, in descending order", 2200);
  await dialog.getByLabel("Filter results", { exact: true }).fill("EMEA");
  await dialog.getByRole("button", { name: "Filter results", exact: true }).click();
  await expect(dialog.getByText(/10 rows · Page 1 of 1/)).toBeVisible();
  await frame("results", "4 / 5  ·  Filter across columns to focus on one region", 2200);
  await dialog.getByRole("button", { name: "Export CSV", exact: true }).click();
  const download = dialog.getByRole("link", { name: "Download CSV", exact: true });
  await expect(download).toBeVisible();
  const csv = await download.evaluate(async link => (await fetch(link.href)).text());
  if (csv.trim().split(/\r?\n/).length !== 31) throw new Error("CSV must contain all 30 rows plus its header");
  await frame("results", "5 / 5  ·  Prepare CSV for the whole snapshot, including other regions", 2600);
  // Deliberately do not claim native file saving: the release gate records its environment failure.
  await writeFile(work + "frames.json", JSON.stringify(frames, null, 2));
  console.log("Captured 7 screenshots and 2 walkthrough frame sequences from the real application.");
} finally {
  if (browser) await browser.close();
  if (server && server.exitCode === null) {
    const stopped = new Promise(resolve => server.once("exit", resolve));
    server.kill("SIGTERM");
    await Promise.race([stopped, wait(10000)]);
    if (server.exitCode === null) { server.kill("SIGKILL"); await stopped; }
  }
  run("docker", ["stop", cid]);
}
