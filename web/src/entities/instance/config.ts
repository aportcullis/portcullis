import { createResource } from "solid-js";

import { authClient } from "@/shared/api/client";

// Install-level login surface (Auth.GetConfig, ADR-0013): whether Google login
// is configured and whether the instance still needs its first admin. Public
// pre-session metadata — fetched once at app start; refetch after an action
// that changes it (bootstrap), or the login route would keep redirecting to
// the stale first-run form.
export const [loginConfig, { refetch: refetchLoginConfig }] = createResource(async () => {
  const res = await authClient.getConfig({});
  return { googleEnabled: res.googleEnabled, needsBootstrap: res.needsBootstrap };
});
