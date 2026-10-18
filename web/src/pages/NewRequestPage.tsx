import type { Component } from "solid-js";
import { Show } from "solid-js";
import { A } from "@solidjs/router";
import { hasPermission, sessionAuthorizationKey } from "@/entities/session/store";
import { CreateRequestForm } from "@/features/request/CreateRequestForm";

/** Assembles a request page within the authenticated application shell. */
const NewRequestPage: Component = () => {
  return <Show when={hasPermission("requests.create")} fallback={<p>Permission required. <A href="/requests" class="underline">Back to requests</A></p>}>
    <Show when={sessionAuthorizationKey()} keyed>{_authorization => <CreateRequestForm />}</Show>
  </Show>;
};
export default NewRequestPage;
