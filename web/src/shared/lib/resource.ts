import type { Resource } from "solid-js";

/** Returns the resource's latest settled value, or undefined while it is unresolved or errored. */
export function readResourceOrUndefined<T>(resource: Resource<T>): T | undefined {
  // Solid's accessor rethrows the fetch error and suspends the nearest Suspense boundary while loading; optional consumers read `latest` only once a value exists, so neither a failure nor a background retry blanks the page.
  if (resource.state === "ready" || resource.state === "refreshing") return resource.latest;
  return undefined;
}
