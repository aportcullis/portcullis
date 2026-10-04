import { describe, expect, it, vi } from "vitest";

import { createOpenFetch } from "@/shared/lib/openFetch";

// The error formatter is injected, because shared/ cannot import an entity and each feature words its failures its own way. The tests pass a trivial one and assert the message reaches the caller unchanged.
const asMessage = (err: unknown) => (err instanceof Error ? err.message : "failed");

function deferred<T>() {
  let resolve = (_value: T): void => { throw new Error("Deferred resolver is not initialized"); };
  let reject = (_reason: unknown): void => { throw new Error("Deferred rejection is not initialized"); };
  const promise = new Promise<T>((settle, fail) => {
    resolve = settle;
    reject = fail;
  });
  return { promise, resolve, reject };
}

const flush = () => new Promise((resolve) => setTimeout(resolve));

describe("createOpenFetch", () => {
  it("delivers the fetch of the CURRENT open and discards a stale one", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    const fetcher = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const onLoaded = vi.fn();
    const openFetch = createOpenFetch(fetcher, onLoaded, asMessage);

    openFetch.handleOpenChange(true);
    openFetch.handleOpenChange(false);
    openFetch.handleOpenChange(true);

    second.resolve("fresh");
    first.resolve("stale");
    await flush();

    expect(onLoaded).toHaveBeenCalledTimes(1);
    expect(onLoaded).toHaveBeenCalledWith("fresh");
    expect(openFetch.loading()).toBe(false);
    expect(openFetch.error()).toBe("");
  });

  it("fences responses that land after the dialog closed", async () => {
    const pending = deferred<string>();
    const onLoaded = vi.fn();
    const openFetch = createOpenFetch(() => pending.promise, onLoaded, asMessage);

    openFetch.handleOpenChange(true);
    expect(openFetch.loading()).toBe(true);
    openFetch.handleOpenChange(false);
    expect(openFetch.loading()).toBe(false);

    pending.resolve("late");
    await flush();

    expect(onLoaded).not.toHaveBeenCalled();
    expect(openFetch.loading()).toBe(false);
  });

  it("fences a stale error and reports only the current open's failure", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    const fetcher = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const openFetch = createOpenFetch(fetcher, vi.fn(), asMessage);

    openFetch.handleOpenChange(true);
    openFetch.handleOpenChange(false);
    openFetch.handleOpenChange(true);
    expect(openFetch.loading()).toBe(true);

    first.reject(new Error("stale failure"));
    await flush();
    expect(openFetch.error()).toBe(""); // the stale error must not surface
    expect(openFetch.loading()).toBe(true);

    second.reject(new Error("current failure"));
    await flush();
    expect(openFetch.error()).not.toBe("");
    expect(openFetch.loading()).toBe(false);
  });

  // A result page change re-reads while the panel stays open; the earlier page's slower answer must not replace the newer one.
  it("drops an earlier refetch that resolves after a later one while the dialog stays open", async () => {
    const earlierPage = deferred<string>();
    const laterPage = deferred<string>();
    const fetcher = vi.fn().mockReturnValueOnce(earlierPage.promise).mockReturnValueOnce(laterPage.promise);
    const onLoaded = vi.fn();
    const openFetch = createOpenFetch(fetcher, onLoaded, asMessage);

    openFetch.handleOpenChange(true);
    openFetch.handleOpenChange(true);
    laterPage.resolve("page 2");
    await flush();
    earlierPage.resolve("page 1");
    await flush();

    expect(onLoaded).toHaveBeenCalledTimes(1);
    expect(onLoaded).toHaveBeenCalledWith("page 2");
  });

  it("keeps loading while the earlier of two open refetches settles first", async () => {
    const earlierPage = deferred<string>();
    const laterPage = deferred<string>();
    const fetcher = vi.fn().mockReturnValueOnce(earlierPage.promise).mockReturnValueOnce(laterPage.promise);
    const openFetch = createOpenFetch(fetcher, vi.fn(), asMessage);

    openFetch.handleOpenChange(true);
    openFetch.handleOpenChange(true);
    earlierPage.reject(new Error("earlier page failed"));
    await flush();

    expect(openFetch.loading()).toBe(true);
    expect(openFetch.error()).toBe("");
  });

  it("captureSession reports staleness across a reopen — follow-up requests can fence themselves", () => {
    const openFetch = createOpenFetch(() => Promise.resolve("value"), vi.fn(), asMessage);
    openFetch.handleOpenChange(true);
    const isSameSession = openFetch.captureSession();
    expect(isSameSession()).toBe(true);
    openFetch.handleOpenChange(false);
    expect(isSameSession()).toBe(false);
  });

  it("fences a mutation started from the dialog on the same session", async () => {
    const openFetch = createOpenFetch(() => Promise.resolve("value"), vi.fn(), asMessage);
    openFetch.handleOpenChange(true);
    const outcome = openFetch.runInSession(() => Promise.resolve("saved"));
    openFetch.handleOpenChange(false);
    expect(await outcome).toEqual({ status: "superseded" });
  });
});
