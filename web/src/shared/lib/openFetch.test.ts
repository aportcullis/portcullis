import { describe, expect, it, vi } from "vitest";

import { createOpenFetch } from "@/shared/lib/openFetch";

// The error formatter is injected, because shared/ cannot import an entity and each feature words its failures its own way. The tests pass a trivial one and assert the message reaches the caller unchanged.
const asMessage = (err: unknown) => (err instanceof Error ? err.message : "failed");

function deferred<T>() {
  let resolve = (_value: T): void => { throw new Error("Deferred resolver is not initialized"); };
  let reject = (_reason: unknown): void => { throw new Error("Deferred rejection is not initialized"); };
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const flush = () => new Promise((r) => setTimeout(r));

describe("createOpenFetch", () => {
  it("delivers the fetch of the CURRENT open and discards a stale one", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    const fetcher = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const onLoaded = vi.fn();
    const f = createOpenFetch(fetcher, onLoaded, asMessage);

    f.handleOpenChange(true);
    f.handleOpenChange(false);
    f.handleOpenChange(true);

    second.resolve("fresh");
    first.resolve("stale");
    await flush();

    expect(onLoaded).toHaveBeenCalledTimes(1);
    expect(onLoaded).toHaveBeenCalledWith("fresh");
    expect(f.loading()).toBe(false);
    expect(f.error()).toBe("");
  });

  it("fences responses that land after the dialog closed", async () => {
    const d = deferred<string>();
    const onLoaded = vi.fn();
    const f = createOpenFetch(() => d.promise, onLoaded, asMessage);

    f.handleOpenChange(true);
    expect(f.loading()).toBe(true);
    f.handleOpenChange(false);
    expect(f.loading()).toBe(false);

    d.resolve("late");
    await flush();

    expect(onLoaded).not.toHaveBeenCalled();
    expect(f.loading()).toBe(false);
  });

  it("fences a stale error and reports only the current open's failure", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    const fetcher = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const f = createOpenFetch(fetcher, vi.fn(), asMessage);

    f.handleOpenChange(true);
    f.handleOpenChange(false);
    f.handleOpenChange(true);
    expect(f.loading()).toBe(true);

    first.reject(new Error("stale failure"));
    await flush();
    expect(f.error()).toBe(""); // the stale error must not surface
    expect(f.loading()).toBe(true);

    second.reject(new Error("current failure"));
    await flush();
    expect(f.error()).not.toBe("");
    expect(f.loading()).toBe(false);
  });

  it("captureSession reports staleness across a reopen — follow-up requests can fence themselves", () => {
    const f = createOpenFetch(() => Promise.resolve("x"), vi.fn(), asMessage);
    f.handleOpenChange(true);
    const isSameSession = f.captureSession();
    expect(isSameSession()).toBe(true);
    f.handleOpenChange(false);
    expect(isSameSession()).toBe(false);
  });

  it("fences a mutation started from the dialog on the same session", async () => {
    const f = createOpenFetch(() => Promise.resolve("x"), vi.fn(), asMessage);
    f.handleOpenChange(true);
    const outcome = f.runInSession(() => Promise.resolve("saved"));
    f.handleOpenChange(false);
    expect(await outcome).toEqual({ status: "superseded" });
  });
});
