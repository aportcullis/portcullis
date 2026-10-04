import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import { describeResultError } from "@/features/request/resultErrors";

// The server maps an expired or evicted snapshot to FailedPrecondition ("result unavailable or expired"); other failures must not suggest expiry.
const formatMessage = (err: unknown) => (err instanceof ConnectError ? err.rawMessage : "Request failed.");
const expiryHint = "Results may have expired or been evicted.";

describe("describeResultError", () => {
  it("adds the expiry hint for an expired or evicted snapshot", () => {
    const message = describeResultError(new ConnectError("result unavailable or expired", Code.FailedPrecondition), formatMessage);
    expect(message).toBe(`result unavailable or expired ${expiryHint}`);
  });

  it("adds the expiry hint when the execution record is not found", () => {
    expect(describeResultError(new ConnectError("not found", Code.NotFound), formatMessage)).toContain(expiryHint);
  });

  it("keeps the server message first", () => {
    expect(describeResultError(new ConnectError("result unavailable or expired", Code.FailedPrecondition), formatMessage)).toMatch(/^result unavailable/);
  });

  it("reports an unavailable server without the expiry hint", () => {
    expect(describeResultError(new ConnectError("temporarily unavailable", Code.Unavailable), formatMessage)).toBe("temporarily unavailable");
  });

  it("reports a busy result worker without the expiry hint", () => {
    expect(describeResultError(new ConnectError("temporarily busy", Code.ResourceExhausted), formatMessage)).not.toContain(expiryHint);
  });

  it("reports a refused permission without the expiry hint", () => {
    expect(describeResultError(new ConnectError("permission denied", Code.PermissionDenied), formatMessage)).not.toContain(expiryHint);
  });

  it("reports a browser network failure without the expiry hint", () => {
    expect(describeResultError(new TypeError("Failed to fetch"), formatMessage)).toBe("Request failed.");
  });

  it("reports an invalid filter without the expiry hint", () => {
    expect(describeResultError(new ConnectError("invalid result query", Code.InvalidArgument), formatMessage)).toBe("invalid result query");
  });
});
