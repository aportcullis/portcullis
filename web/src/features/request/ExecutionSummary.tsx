import type { Component } from "solid-js";
import type { QueryExecution } from "@/gen/portcullis/v1/query_executions_pb";
import { formatExecutionDuration } from "@/features/request/executionMetrics";

/** Displays durable execution metrics and their server measurement scope. */
export const ExecutionSummary: Component<{ execution: QueryExecution }> = (props) => (
  <section aria-label="Execution summary" class="content-surface">
    <dl class="flex flex-wrap gap-x-10 gap-y-4">
      <div><dt class="text-sm text-muted-foreground">Execution time</dt><dd aria-label="Recorded execution time" class="mt-1 text-2xl font-semibold tabular-nums">{formatExecutionDuration(props.execution)}</dd></div>
      <div><dt class="text-sm text-muted-foreground">Rows affected</dt><dd class="mt-1 text-2xl font-semibold tabular-nums">{props.execution.rowsAffected.toString()}</dd></div>
    </dl>
    <p class="mt-3 text-xs text-muted-foreground">Server time includes DB connection, SQL execution, result collection and snapshot storage. Approval waiting, browser rendering and later paging/sorting are excluded.</p>
  </section>
);
