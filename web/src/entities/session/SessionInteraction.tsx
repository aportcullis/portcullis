import { Show, createSignal, onCleanup, type ParentProps } from "solid-js";

import { sessionInteractionKey } from "@/entities/session/store";
import { observeAuthorizationFailures } from "@/shared/api/authorization";

/** Discards route-owned local inputs on authority changes or admission refusals without retrying writes. */
export function SessionInteraction(props: ParentProps) {
  return <Show when={sessionInteractionKey()} keyed>{_interaction => <AdmittedInteraction>{props.children}</AdmittedInteraction>}</Show>;
}

/** Ends a refused interaction before metadata recovery can recreate its drafts or delivery. */
function AdmittedInteraction(props: ParentProps) {
  const [refused, setRefused] = createSignal(false);
  const unsubscribe = observeAuthorizationFailures(failure => {
    if (failure.kind === "session" || failure.scope === "interaction") setRefused(true);
  });
  onCleanup(unsubscribe);
  return <Show when={!refused()} fallback={<p role="alert">Permission changed. Local inputs and setup delivery were cleared. Reload to check your current access.</p>}>{props.children}</Show>;
}
