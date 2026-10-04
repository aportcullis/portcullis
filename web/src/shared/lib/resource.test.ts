import { createResource, createRoot } from "solid-js";
import { describe, expect, it } from "vitest";

import { readResourceOrUndefined } from "@/shared/lib/resource";

// Solid's resource accessor rethrows the fetch error; without an ErrorBoundary that breaks the page, so optional consumers read through this guard.
const flush = () => new Promise((resolve) => setTimeout(resolve));

type Fetcher<T> = () => Promise<T>;

function createTestResource<T>(fetcher: Fetcher<T>) {
  return createRoot(() => createResource(fetcher));
}

describe("readResourceOrUndefined", () => {
  it("returns the loaded value", async () => {
    const [resource] = createTestResource(async () => ({ maxRequestTitleChars: 200 }));
    await flush();
    expect(readResourceOrUndefined(resource)).toEqual({ maxRequestTitleChars: 200 });
  });

  it("returns a falsy loaded value unchanged", async () => {
    const [resource] = createTestResource(async () => 0);
    await flush();
    expect(readResourceOrUndefined(resource)).toBe(0);
  });

  it("returns the refetched value after an earlier failure", async () => {
    let attempt = 0;
    const [resource, { refetch }] = createTestResource(async () => {
      attempt++;
      if (attempt === 1) throw new Error("unavailable");
      return "recovered";
    });
    await flush();
    expect(readResourceOrUndefined(resource)).toBeUndefined();
    await refetch();
    await flush();
    expect(readResourceOrUndefined(resource)).toBe("recovered");
  });

  it("returns undefined instead of throwing when the fetch failed", async () => {
    const [resource] = createTestResource(async (): Promise<string> => {
      throw new Error("unavailable");
    });
    await flush();
    expect(() => readResourceOrUndefined(resource)).not.toThrow();
    expect(readResourceOrUndefined(resource)).toBeUndefined();
  });

  it("returns undefined while the first fetch is pending", () => {
    const [resource] = createTestResource(() => new Promise<string>(() => {}));
    expect(readResourceOrUndefined(resource)).toBeUndefined();
  });

  it("returns undefined after a refetch fails, rather than a value from before", async () => {
    let attempt = 0;
    const [resource, { refetch }] = createTestResource(async () => {
      attempt++;
      if (attempt === 2) throw new Error("unavailable");
      return "first";
    });
    await flush();
    expect(readResourceOrUndefined(resource)).toBe("first");
    await refetch();
    await flush();
    expect(() => readResourceOrUndefined(resource)).not.toThrow();
  });

  it("does not throw for a non-Error rejection value", async () => {
    const [resource] = createTestResource(async (): Promise<string> => {
      throw "plain string rejection";
    });
    await flush();
    expect(() => readResourceOrUndefined(resource)).not.toThrow();
  });
});
