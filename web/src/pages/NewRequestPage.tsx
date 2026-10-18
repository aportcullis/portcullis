import type { Component } from "solid-js";
import { Show } from "solid-js";
import { A } from "@solidjs/router";
import { hasPermission } from "@/entities/session/store";
import { SessionInteraction } from "@/entities/session/SessionInteraction";
import { CreateRequestForm } from "@/features/request/CreateRequestForm";

/** Assembles a request page within the authenticated application shell. */
const NewRequestPage: Component = () => {
  return <Show when={hasPermission("requests.create")} fallback={<p>Permission required. <A href="/requests" class="underline">Back to requests</A></p>}>
    <SessionInteraction><CreateRequestForm /></SessionInteraction>
  </Show>;
};
export default NewRequestPage;
