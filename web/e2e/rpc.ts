import type { Page, Route } from "@playwright/test";

/** Connect error codes the browser scenarios simulate, with the HTTP status the Connect protocol maps them to. */
const connectErrorStatus = {
  unavailable: 503,
  not_found: 404,
  permission_denied: 403,
  unauthenticated: 401,
  internal: 500,
} as const;

export type ConnectErrorCode = keyof typeof connectErrorStatus;

/** Answers an intercepted Connect unary call with the given error, as the server would. */
export async function fulfillConnectError(route: Route, code: ConnectErrorCode, message = "simulated failure"): Promise<void> {
  await route.fulfill({
    status: connectErrorStatus[code],
    contentType: "application/json",
    body: JSON.stringify({ code, message }),
  });
}

/** Answers an intercepted binary Connect unary call with an empty response message (every field unset). */
export async function fulfillEmptyMessage(route: Route): Promise<void> {
  await route.fulfill({ status: 200, contentType: "application/proto", body: Buffer.alloc(0) });
}

/** Makes every call to one procedure fail until the returned function restores the real server. */
export async function failProcedure(page: Page, procedure: string, code: ConnectErrorCode): Promise<() => Promise<void>> {
  const pattern = `**/portcullis.v1.${procedure}`;
  const handler = (route: Route) => fulfillConnectError(route, code);
  await page.route(pattern, handler);
  return () => page.unroute(pattern, handler);
}
