import { describe, expect, it, vi } from "vitest";

import { createOpenFetch } from "@/features/connection/openFetch";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
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
    const f = createOpenFetch(fetcher, onLoaded);

    f.handleOpenChange(true); // open #1 — will resolve late
    f.handleOpenChange(false);
    f.handleOpenChange(true); // open #2 — the current owner

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
    const f = createOpenFetch(() => d.promise, onLoaded);

    f.handleOpenChange(true);
    expect(f.loading()).toBe(true);
    f.handleOpenChange(false);

    d.resolve("late");
    await flush();

    expect(onLoaded).not.toHaveBeenCalled();
  });

  it("fences a stale error and reports only the current open's failure", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    const fetcher = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const f = createOpenFetch(fetcher, vi.fn());

    f.handleOpenChange(true);
    f.handleOpenChange(false);
    f.handleOpenChange(true);

    first.reject(new Error("stale failure"));
    await flush();
    expect(f.error()).toBe(""); // the stale error must not surface

    second.reject(new Error("current failure"));
    await flush();
    expect(f.error()).not.toBe("");
    expect(f.loading()).toBe(false);
  });

  it("guard() reports staleness across a reopen — follow-up requests can fence themselves", () => {
    const f = createOpenFetch(() => Promise.resolve("x"), vi.fn());
    f.handleOpenChange(true);
    const current = f.guard();
    expect(current()).toBe(true);
    f.handleOpenChange(false); // reopen/close invalidates captured guards
    expect(current()).toBe(false);
  });
});
