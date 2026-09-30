import type { ConnectionSummary } from "@/gen/portcullis/v1/connections_pb";

// Resolve the open editor by ID against refreshed rows for current version tokens; retain the captured row when reload fails.
export function resolveEditTarget(
  rows: readonly ConnectionSummary[],
  captured: ConnectionSummary | undefined,
): ConnectionSummary | undefined {
  if (!captured) return undefined;
  return rows.find((row) => row.id === captured.id) ?? captured;
}
