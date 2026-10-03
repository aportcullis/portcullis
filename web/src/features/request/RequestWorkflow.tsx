import type { Component } from "solid-js";
import { For, Show, createMemo } from "solid-js";
import { A } from "@solidjs/router";
import type { AccessRequest } from "@/gen/portcullis/v1/access_requests_pb";
import { requestWorkflow } from "@/features/request/workflow";
import { stateLabel } from "@/entities/request/model";

/** Shows a request's stage DAG using only the authorized list summary. */
export const RequestWorkflow: Component<{ request: AccessRequest; mayOpenDetails: boolean }> = (props) => {
  const flow = createMemo(() => requestWorkflow(props.request));
  return <section id={`workflow-${props.request.id}`} aria-label={`Workflow for ${props.request.title || "Untitled request"}`} class="request-workflow">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-semibold">Request workflow · {stateLabel(props.request.effectiveState)}</h3>
      <Show when={props.mayOpenDetails}><A class="text-sm font-medium text-primary underline underline-offset-4" href={`/requests/${props.request.id}`}>Open request details →</A></Show>
    </div>
    <ol class="workflow-nodes" aria-label="Request stages">
      <For each={flow().nodes}>{(node, index) => <li class="workflow-node" data-status={node.status} aria-current={node.status === "current" ? "step" : undefined}>
        <span class="workflow-number" aria-hidden="true">{node.status === "complete" ? "✓" : index() + 1}</span>
        <div><div class="font-semibold">{node.label}</div><p>{node.detail}</p><span class="workflow-status">{node.status.replace("-", " ")}</span></div>
      </li>}</For>
    </ol>
    <p class="mt-4 text-xs text-muted-foreground">{flow().notice}</p>
    <p class="mt-1 text-xs text-muted-foreground">Refreshes with the request list; elapsed time and complete transition history are not available here.</p>
  </section>;
};
