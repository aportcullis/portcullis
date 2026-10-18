import { createRoot } from "solid-js";
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import { createManagementQuery } from "@/entities/administration/query";

/** Exposes deterministic response ordering without timers. */
function deferred<Value>() {
  let settle: (value: Value) => void = () => { throw new Error("not initialized"); };
  const promise = new Promise<Value>(resolve => { settle = resolve; });
  return { promise, settle };
}

describe("management route data ownership", () => {
  it.each([Code.PermissionDenied, Code.Unauthenticated, Code.NotFound])("discards route-owned delivery when a current protected read refuses admission %s", async code => {
    await createRoot(async dispose => {
      let delivery = "fresh setup delivery";
      const query = createManagementQuery<string>(() => {}, () => { delivery = ""; });
      await query.load(async () => "authorized account");
      await query.load(async () => { throw new ConnectError("private diagnostic", code); });
      expect(delivery).toBe(""); expect(query.data()).toBeUndefined(); dispose();
    });
  });
  it("retains local delivery on an ordinary detail retrieval outage without restoring protected data", async () => {
    await createRoot(async dispose => {
      let delivery = "fresh setup delivery";
      const query = createManagementQuery<string>(() => {}, () => { delivery = ""; });
      await query.load(async () => { throw new ConnectError("temporary", Code.Unavailable); });
      expect(delivery).toBe("fresh setup delivery"); expect(query.data()).toBeUndefined(); dispose();
    });
  });
  it("ignores an old refused read after a newer account detail succeeds", async () => {
    await createRoot(async dispose => {
      let refusals = 0;
      const query = createManagementQuery<string>(() => {}, () => { refusals++; });
      const delayed = deferred<string>();
      const old = query.load(async () => { await delayed.promise; throw new ConnectError("old refusal", Code.NotFound); });
      await query.load(async () => "fresh account");
      delayed.settle("finished"); await old;
      expect(refusals).toBe(0); expect(query.data()).toBe("fresh account"); dispose();
    });
  });
  it("re-resolves session admission after a read discovers session revocation", async () => {
    await createRoot(async dispose => {
      let admissions = 0;
      const query = createManagementQuery<string>(() => { admissions++; });
      await query.load(async () => "authorized details");
      await query.load(async () => { throw new ConnectError("expired", Code.Unauthenticated); });
      expect(query.data()).toBeUndefined(); expect(admissions).toBe(1); dispose();
    });
  });
  it("retains only the latest page when responses arrive out of order", async () => {
    await createRoot(async dispose => {
      const query = createManagementQuery<string>();
      const delayed = deferred<string>();
      const old = query.load(() => delayed.promise);
      await query.load(async () => "page two");
      delayed.settle("page one"); await old;
      expect(query.data()).toBe("page two"); dispose();
    });
  });
  it("fences a previous identity and clears pending state immediately", async () => {
    await createRoot(async dispose => {
      const query = createManagementQuery<string>(); const delayed = deferred<string>();
      const old = query.load(() => delayed.promise); query.reset();
      expect(query.loading()).toBe(false);
      delayed.settle("private data"); await old;
      expect(query.data()).toBeUndefined(); dispose();
    });
  });
  it.each([Code.PermissionDenied, Code.Unauthenticated, Code.NotFound, Code.Aborted])("clears prior protected rows and counts after refusal %s", async code => {
    await createRoot(async dispose => {
      const query = createManagementQuery<{ rows: string[]; total: number }>();
      await query.load(async () => ({ rows: ["private"], total: 9 }));
      await query.load(async () => { throw new ConnectError("raw source secret", code); });
      expect(query.data()).toBeUndefined(); expect(query.error()).not.toContain("secret"); dispose();
    });
  });
  it("recovers after service activation without retaining an old error", async () => {
    await createRoot(async dispose => {
      const query = createManagementQuery<string>();
      await query.load(async () => { throw new ConnectError("pending", Code.Unavailable); });
      await query.load(async () => "authorized");
      expect(query.data()).toBe("authorized"); expect(query.error()).toBe(""); dispose();
    });
  });
});
