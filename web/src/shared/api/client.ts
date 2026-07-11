import type { Interceptor } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { Auth } from "@/gen/portcullis/v1/auth_pb";
import { csrfToken } from "@/shared/lib/csrf";

// Every call echoes the CSRF cookie in the X-CSRF-Token header (double submit,
// ADR-0006). Public procedures ignore the header, so sending it untargeted is
// harmless and saves per-call bookkeeping.
const csrf: Interceptor = (next) => (req) => {
  const token = csrfToken();
  if (token !== "") {
    req.header.set("X-CSRF-Token", token);
  }
  return next(req);
};

// Same-origin transport: the SPA is embedded in the Go binary (dev uses the
// vite proxy), and the session rides the __Host- cookies.
const transport = createConnectTransport({ baseUrl: "/", interceptors: [csrf] });

export const authClient = createClient(Auth, transport);
