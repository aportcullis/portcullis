import { describe, expect, it, vi } from "vitest";

import { createDialogSession } from "@/shared/lib/dialogSession";

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


describe("createDialogSession", () => {
  it("reports work that outlives its session as superseded, not as a result", async () => {
    const dialog = createDialogSession();
    const save = deferred<string>();

    dialog.discardSession();
    const outcome = dialog.runInSession(() => save.promise);
    dialog.discardSession();
    dialog.discardSession();

    save.resolve("saved");
    expect(await outcome).toEqual({ status: "superseded" });
  });

  it("reports a failure that outlives its session as superseded, so it cannot overwrite the new error", async () => {
    const dialog = createDialogSession();
    const save = deferred<string>();

    dialog.discardSession();
    const outcome = dialog.runInSession(() => save.promise);
    dialog.discardSession();

    save.reject(new Error("archived"));
    expect(await outcome).toEqual({ status: "superseded" });
  });

  it("supersedes even without a reopen — a closed dialog has nothing to show", async () => {
    const dialog = createDialogSession();
    const save = deferred<number>();

    dialog.discardSession();
    const outcome = dialog.runInSession(() => save.promise);
    dialog.discardSession();

    save.resolve(7);
    expect(await outcome).toEqual({ status: "superseded" });
  });

  it("delivers the result when the session is still the one that asked", async () => {
    const dialog = createDialogSession();
    dialog.discardSession();

    await expect(dialog.runInSession(() => Promise.resolve("saved"))).resolves.toEqual({
      status: "ok",
      value: "saved",
    });

    const refusal = new Error("refused");
    await expect(dialog.runInSession(() => Promise.reject(refusal))).resolves.toEqual({
      status: "failed",
      error: refusal,
    });
  });

  it("keeps two overlapping sessions apart: only the later one is answered", async () => {
    const dialog = createDialogSession();
    const first = deferred<string>();
    const second = deferred<string>();

    dialog.discardSession();
    const stale = dialog.runInSession(() => first.promise);
    dialog.discardSession();
    const current = dialog.runInSession(() => second.promise);

    second.resolve("second");
    first.resolve("first");
    await flush();

    expect(await stale).toEqual({ status: "superseded" });
    expect(await current).toEqual({ status: "ok", value: "second" });
  });

  it("captureSession answers for callers that fence their own follow-up work", () => {
    const dialog = createDialogSession();
    dialog.discardSession();
    const isSameSession = dialog.captureSession();
    expect(isSameSession()).toBe(true);
    dialog.discardSession();
    expect(isSameSession()).toBe(false);
  });

  it("runs the work exactly once regardless of the fence", async () => {
    const dialog = createDialogSession();
    const work = vi.fn().mockResolvedValue("x");

    dialog.discardSession();
    const outcome = dialog.runInSession(work);
    dialog.discardSession();
    await outcome;

    expect(work).toHaveBeenCalledTimes(1);
  });
});
