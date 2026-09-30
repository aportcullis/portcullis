import type { Interceptor } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { AccessRequests } from "@/gen/portcullis/v1/access_requests_pb";
import { Auth } from "@/gen/portcullis/v1/auth_pb";
import { ConnectionPolicies } from "@/gen/portcullis/v1/connection_policies_pb";
import { Connections } from "@/gen/portcullis/v1/connections_pb";
import { readCSRFTokenCookie } from "@/shared/lib/csrf";

// Every call echoes the CSRF cookie in the X-CSRF-Token header (double submit, ADR-0006). Public procedures ignore the header, so sending it untargeted is harmless and saves per-call bookkeeping.
const csrf: Interceptor = (next) => (req) => {
  const token = readCSRFTokenCookie();
  if (token !== "") {
    req.header.set("X-CSRF-Token", token);
  }
  return next(req);
};

// Use binary Connect encoding so JSON escaping cannot inflate accepted SQL beyond the 64 KiB request limit (ADR-0010). Sessions use same-origin cookies.
const transport = createConnectTransport({ baseUrl: "/", interceptors: [csrf], useBinaryFormat: true });

export const authClient = createClient(Auth, transport);
export const connectionsClient = createClient(Connections, transport);
export const policiesClient = createClient(ConnectionPolicies, transport);
export const requestsClient = createClient(AccessRequests, transport);
