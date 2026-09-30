import { Code, ConnectError } from "@connectrpc/connect";

// Retry only ResourceExhausted reads with backoff while the per-IP bucket refills; surface authentication and availability errors immediately.

export type RetryOptions = {
  // attempts counts the FIRST call too, so 3 means one call plus two retries.
  attempts?: number;
  // baseDelayMs is the first backoff step; each further wait doubles it. The server refills one token per second, so the first step alone usually suffices.
  baseDelayMs?: number;
};

const defaults: Required<RetryOptions> = { attempts: 3, baseDelayMs: 1000 };

function isThrottled(err: unknown): boolean {
  return err instanceof ConnectError && err.code === Code.ResourceExhausted;
}

const wait = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

export async function retryOnThrottle<T>(call: () => Promise<T>, options?: RetryOptions): Promise<T> {
  const { attempts, baseDelayMs } = { ...defaults, ...options };
  let delay = baseDelayMs;
  for (let attempt = 1; ; attempt++) {
    try {
      return await call();
    } catch (err) {
      if (!isThrottled(err) || attempt >= attempts) throw err;
      await wait(delay);
      delay *= 2;
    }
  }
}
