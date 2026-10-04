import type { Component } from "solid-js";
import { For, Show } from "solid-js";

import { actorLabel } from "@/entities/request/model";
import type { AccessRequest, AccessRequestPayload } from "@/gen/portcullis/v1/access_requests_pb";
import { Badge } from "@/shared/ui/badge";

/** Shows what a reviewer decides on: the body, SQL and parameters (or redacted SQL), the submitted target snapshot and the decisions so far. */
export const RequestEvidence: Component<{ request: AccessRequest; payload: AccessRequestPayload | undefined }> = (props) => (
  <div class="content-surface request-evidence">
    <h2 class="text-base font-semibold">Request evidence</h2>
    <Show when={props.payload} fallback={
      <div class="flex flex-col gap-1">
        <span class="text-sm font-medium">SQL (redacted)</span>
        <pre class="overflow-x-auto rounded-md border border-input bg-muted p-3 text-sm">{props.request.redactedSql || "—"}</pre>
      </div>
    }>
      {(payload) => (
        <>
          <Show when={payload().body !== ""}>
            <div class="flex flex-col gap-1">
              <span class="text-sm font-medium">Body</span>
              <p class="whitespace-pre-wrap break-words rounded-md border border-input p-3 text-sm">{payload().body}</p>
            </div>
          </Show>
          <div class="flex flex-col gap-1">
            <span class="text-sm font-medium">SQL</span>
            <pre class="overflow-x-auto rounded-md border border-input bg-muted p-3 font-mono text-sm">{payload().sql}</pre>
          </div>
          <Show when={payload().params.length > 0}>
            <div class="flex flex-col gap-1">
              <span class="text-sm font-medium">Parameters</span>
              <For each={payload().params}>
                {(param) => (
                  <div class="text-sm text-muted-foreground">
                    <span class="font-mono">:{param.name}</span> ({param.type}) ={" "}
                    {param.type === "null" ? "null" : param.value}
                  </div>
                )}
              </For>
            </div>
          </Show>
        </>
      )}
    </Show>

    {/* Show the submitted target snapshot rather than today’s mutable connection; drafts have no snapshot (PRD §4.3). */}
    <Show when={props.request.connectionFingerprint !== ""}>
      <div class="flex flex-col gap-1">
        <span class="text-sm font-medium">Target at submit</span>
        <div class="text-sm text-muted-foreground">
          {props.request.connectionName} ({props.request.connectionDbType}) · config v
          {props.request.connectionConfigVersion.toString()}
        </div>
        <div class="font-mono text-xs text-muted-foreground break-all">
          {props.request.connectionFingerprint}
        </div>
      </div>
    </Show>

    <div class="flex flex-col gap-1">
      <span class="text-sm font-medium">
        Approvals ({props.request.validApprovals.toString()} / {props.request.requiredApprovals})
      </span>
      <Show when={props.request.approvals.length > 0} fallback={<span class="text-sm text-muted-foreground">No decisions yet.</span>}>
        <For each={props.request.approvals}>
          {(decision) => (
            <div class="flex items-center gap-2 text-sm">
              <Badge
                variant={
                  decision.decision === "rejected"
                    ? "destructive"
                    : decision.valid
                      ? "default"
                      : "outline"
                }
              >
                {decision.decision}
              </Badge>
              <span>{actorLabel(decision.approver)}</span>
              <Show when={decision.reason !== ""}>
                <span class="text-muted-foreground">— {decision.reason}</span>
              </Show>
              <Show when={decision.decision === "approved" && !decision.valid}>
                <span class="text-xs text-muted-foreground">(no longer counts)</span>
              </Show>
            </div>
          )}
        </For>
      </Show>
    </div>
  </div>
);
