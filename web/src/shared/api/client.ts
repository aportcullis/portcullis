import type { Interceptor } from "@connectrpc/connect";
import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { AccessRequests } from "@/gen/portcullis/v1/access_requests_pb";
import { Bindings, Roles, Users } from "@/gen/portcullis/v1/administration_pb";
import { Auth } from "@/gen/portcullis/v1/auth_pb";
import { ConnectionPolicies } from "@/gen/portcullis/v1/connection_policies_pb";
import { Connections } from "@/gen/portcullis/v1/connections_pb";
import { QueryExecutions } from "@/gen/portcullis/v1/query_executions_pb";
import { readCSRFTokenCookie } from "@/shared/lib/csrf";
import { notifyAuthorizationFailure } from "@/shared/api/authorization";

// Every call echoes the CSRF cookie in the X-CSRF-Token header (double submit, ADR-0006). Public procedures ignore the header, so sending it untargeted is harmless and saves per-call bookkeeping.
const csrf: Interceptor = (next) => (req) => {
  const token = readCSRFTokenCookie();
  if (token !== "") {
    req.header.set("X-CSRF-Token", token);
  }
  return next(req);
};

// Use binary Connect encoding so JSON escaping cannot inflate accepted SQL beyond the 64 KiB request limit (ADR-0010). Sessions use same-origin cookies.
const admission: Interceptor = next => async request => {
  try { return await next(request); }
  catch (error) {
    if (request.service.typeName !== Auth.typeName && error instanceof ConnectError) {
      if (error.code === Code.Unauthenticated) notifyAuthorizationFailure("session");
      if (error.code === Code.PermissionDenied) notifyAuthorizationFailure("permission");
    }
    throw error;
  }
};
const transport = createConnectTransport({ baseUrl: "/", interceptors: [csrf, admission], useBinaryFormat: true });

export const authClient = createClient(Auth, transport);
export const connectionsClient = createClient(Connections, transport);
export const policiesClient = createClient(ConnectionPolicies, transport);
export const requestsClient = createClient(AccessRequests, transport);
export const executionsClient = createClient(QueryExecutions, transport);

export const usersClient = createClient(Users, transport);
export const rolesClient = createClient(Roles, transport);
export const bindingsClient = createClient(Bindings, transport);
