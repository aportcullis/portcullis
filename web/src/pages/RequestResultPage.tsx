import type { Component } from "solid-js";
import { Show } from "solid-js";
import { A, useParams } from "@solidjs/router";
import { hasPermission } from "@/entities/session/store";
import { ResultPanel } from "@/features/request/ResultPanel";

/** Assembles a request page within the authenticated application shell. */
const RequestResultPage: Component = () => {
  const params = useParams<{ id: string }>();
  return <Show when={hasPermission("requests.get")} fallback={<p>Permission required. <A href="/requests" class="underline">Back to requests</A></p>}>
    <ResultPanel requestId={params.id} />
  </Show>;
};
export default RequestResultPage;
