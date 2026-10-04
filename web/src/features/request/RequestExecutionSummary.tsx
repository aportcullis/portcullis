import type { Component } from "solid-js";
import { Show, createEffect, createSignal, onCleanup } from "solid-js";
import type { QueryExecution } from "@/gen/portcullis/v1/query_executions_pb";
import { executionsClient } from "@/shared/api/client";
import { createOpenFetch } from "@/shared/lib/openFetch";
import { errorMessage } from "@/shared/api/errors";
import { ExecutionSummary } from "@/features/request/ExecutionSummary";

/** Reads requester-only durable metrics for a completed request with fenced responses. */
export const RequestExecutionSummary: Component<{ requestId: string }> = (props) => {
  const [execution, setExecution] = createSignal<QueryExecution>();
  const read = createOpenFetch(() => executionsClient.get({ requestId: props.requestId }), setExecution, errorMessage);
  createEffect(() => { void props.requestId; setExecution(); read.handleOpenChange(true); });
  onCleanup(() => read.handleOpenChange(false));
  return <>
    <Show when={read.loading()}><p role="status" class="text-sm text-muted-foreground">Loading execution time…</p></Show>
    <Show when={read.error()}><p role="alert" class="text-sm text-destructive">Execution summary unavailable. {read.error()}</p></Show>
    <Show when={execution()}>{value => <ExecutionSummary execution={value()} />}</Show>
  </>;
};
