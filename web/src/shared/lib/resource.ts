import type { Resource } from "solid-js";

/** Returns the resource's latest settled value, or undefined while it is unresolved or errored. */
export function readResourceOrUndefined<T>(resource: Resource<T>): T | undefined {
  // Solid's accessor rethrows the fetch error; checking state first keeps optional consumers rendering without an ErrorBoundary.
  if (resource.state === "errored") return undefined;
  return resource();
}
