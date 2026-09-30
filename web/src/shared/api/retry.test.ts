import { Code, ConnectError } from "@connectrpc/connect";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { retryOnThrottle } from "@/shared/api/retry";

// Retry ResourceExhausted reads only; authentication and server faults surface immediately.
const throttled = () => new ConnectError("slow down", Code.ResourceExhausted);

describe("retryOnThrottle", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("returns the value when the call succeeds outright", async () => {
    const call = vi.fn().mockResolvedValue("ok");
    await expect(retryOnThrottle(call)).resolves.toBe("ok");
    expect(call).toHaveBeenCalledTimes(1);
  });

  it("retries a throttled call and resolves once the bucket refills", async () => {
    const call = vi.fn().mockRejectedValueOnce(throttled()).mockResolvedValue("ok");
    const pending = retryOnThrottle(call);
    await vi.runAllTimersAsync();
    await expect(pending).resolves.toBe("ok");
    expect(call).toHaveBeenCalledTimes(2);
  });

  it("waits before retrying instead of hammering the bucket it just drained", async () => {
    const call = vi.fn().mockRejectedValue(throttled());
    const pending = retryOnThrottle(call).catch(() => "gave up");

    await Promise.resolve();
    expect(call).toHaveBeenCalledTimes(1);
    await vi.runAllTimersAsync();
    await pending;
    expect(call.mock.calls.length).toBeGreaterThan(1);
  });

  it("gives up after the attempt budget and rethrows the throttle", async () => {
    const call = vi.fn().mockRejectedValue(throttled());
    const pending = retryOnThrottle(call, { attempts: 3 });
    const settled = pending.then(
      () => "resolved",
      (err: unknown) => err,
    );
    await vi.runAllTimersAsync();
    const err = await settled;
    expect(err).toBeInstanceOf(ConnectError);
    expect(call).toHaveBeenCalledTimes(3);
  });

  it("does not retry an authentication rejection", async () => {
    const call = vi.fn().mockRejectedValue(new ConnectError("nope", Code.Unauthenticated));
    const settled = retryOnThrottle(call).then(
      () => "resolved",
      (err: unknown) => err,
    );
    await vi.runAllTimersAsync();
    await expect(settled).resolves.toBeInstanceOf(ConnectError);
    expect(call).toHaveBeenCalledTimes(1);
  });

  it("does not retry a genuine outage", async () => {
    const call = vi.fn().mockRejectedValue(new ConnectError("boom", Code.Unavailable));
    const settled = retryOnThrottle(call).then(
      () => "resolved",
      (err: unknown) => err,
    );
    await vi.runAllTimersAsync();
    await expect(settled).resolves.toBeInstanceOf(ConnectError);
    expect(call).toHaveBeenCalledTimes(1);
  });
});
