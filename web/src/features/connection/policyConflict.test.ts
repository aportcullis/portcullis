import { describe, expect, it } from "vitest";

import { conflictMessage } from "@/features/connection/policyConflict";


describe("conflictMessage", () => {
  it("names the new version when the refresh succeeded", () => {
    const message = conflictMessage({ status: "refreshed", version: 7n, conflicts: [] });
    expect(message).toContain("v7");
    expect(message).toMatch(/save again|again/i);
    expect(message).toMatch(/kept/i);
  });

  it("names the fields the other admin also changed, so the admin knows what to re-decide", () => {
    const message = conflictMessage({
      status: "refreshed",
      version: 7n,
      conflicts: ["write approvals", "max rows"],
    });
    expect(message).toContain("write approvals");
    expect(message).toContain("max rows");

    expect(message).toMatch(/theirs/i);
  });

  it("reports both facts when the refresh failed", () => {
    const message = conflictMessage({ status: "stale", reason: "permission denied" });
    // The save was refused because the policy moved…
    expect(message).toMatch(/another admin|changed|moved/i);
    // …and the new version could not be read, so retrying here cannot work.
    expect(message).toContain("permission denied");
    expect(message).toMatch(/reopen|close/i);
  });

  it("does not promise a retry it cannot honour", () => {
    const stale = conflictMessage({ status: "stale", reason: "network error" });
    expect(stale).not.toMatch(/save again/i);
  });

  it("keeps the refresh failure's own words", () => {
    for (const reason of ["network error", "permission denied", "unavailable"]) {
      expect(conflictMessage({ status: "stale", reason })).toContain(reason);
    }
  });
});
