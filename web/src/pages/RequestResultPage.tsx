import type { Component } from "solid-js";
import { Show } from "solid-js";
import { A, useParams } from "@solidjs/router";
import { hasPermission, sessionAuthorizationKey } from "@/entities/session/store";
import { ResultPanel } from "@/features/result/ResultPanel";

/** Assembles a request page within the authenticated application shell. */
const RequestResultPage: Component = () => {
  const params = useParams<{ id: string }>();
  return <Show when={hasPermission("requests.get")} fallback={<p>Permission required. <A href="/requests" class="underline">Back to requests</A></p>}>
    <Show when={sessionAuthorizationKey()} keyed>{_authorization => <ResultPanel requestId={params.id} />}</Show>
  </Show>;
};
export default RequestResultPage;
