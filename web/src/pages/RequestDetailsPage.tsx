import type { Component } from "solid-js";
import { Show } from "solid-js";
import { A, useParams } from "@solidjs/router";
import { hasPermission } from "@/entities/session/store";
import { RequestDetailsPanel } from "@/features/request/RequestDetailsPanel";

/** Assembles a request page within the authenticated application shell. */
const RequestDetailsPage: Component = () => {
  const params = useParams<{ id: string }>();
  return <Show when={hasPermission("requests.get")} fallback={<p>Permission required. <A href="/requests" class="underline">Back to requests</A></p>}>
    <RequestDetailsPanel requestId={params.id} />
  </Show>;
};
export default RequestDetailsPage;
