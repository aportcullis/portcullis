import type { QueryExecution } from "@/gen/portcullis/v1/query_executions_pb";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { isExecutionOutcomeState } from "@/entities/request/model";

/** Formats the recorded server interval without int64 precision loss. */
export function formatExecutionDuration(execution: Pick<QueryExecution, "state" | "durationMs">): string {
  if (execution.state === AccessRequestState.EXECUTING) return "Running";
  const terminal = isExecutionOutcomeState(execution.state);
  const milliseconds = execution.durationMs;
  if (!terminal || milliseconds < 0n || (execution.state === AccessRequestState.OUTCOME_UNKNOWN && milliseconds === 0n)) return "Not available";
  if (milliseconds === 0n) return "<1 ms";
  if (milliseconds < 1000n) return `${milliseconds} ms`;
  return `${milliseconds / 1000n}.${String(milliseconds % 1000n).padStart(3, "0")} s`;
}
