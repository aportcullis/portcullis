import { createResource } from "solid-js";

import { authClient } from "@/shared/api/client";
import { retryOnThrottle } from "@/shared/api/retry";

// Fetch public instance flags and server limits at startup; reload after bootstrap so routing reflects the initialized installation.
export const [loginConfig, { refetch: refetchLoginConfig }] = createResource(async () => {
  // Retried on a throttle: this is one of the two reads every page load fires, so it is the first thing a drained rate-limit bucket hits (ADR-0010), and a transient shed must not render as "the server could not be reached".
  const res = await retryOnThrottle(() => authClient.getConfig({}));
  return {
    googleEnabled: res.googleEnabled,
    needsBootstrap: res.needsBootstrap,
    // The reason cap comes from the server that enforces it (never a local literal — two copies drift). Undefined until the fetch lands, in which case the input is simply unbounded and the server still refuses.
    maxApprovalReasonChars: res.maxApprovalReasonChars,
  };
});
