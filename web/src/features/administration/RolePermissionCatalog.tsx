import { For, Show, createSignal } from "solid-js";

import { listManagedPermissions } from "@/entities/administration/api";
import { ManagementFeedback, createManagementAction } from "@/features/administration/controls";
import { Button } from "@/shared/ui/button";

/** Owns one revision's protected catalog independently of locally authored Role inputs. */
export function RolePermissionCatalog(props: { onAdmissionFailure: () => void }) {
  const [catalog, setCatalog] = createSignal<string[]>([]);
  const action = createManagementAction(props.onAdmissionFailure);
  return <>
    <Button type="button" variant="outline" disabled={action.busy() || action.locked()} onClick={() => void action.run(listManagedPermissions, "").then(result => { if (result) setCatalog(result.value.permissions); })}>Load permission catalog</Button>
    <ManagementFeedback error={action.error()} />
    <Show when={catalog().length}><ul aria-label="Permission catalog" class="max-h-48 overflow-y-auto text-sm font-mono"><For each={catalog()}>{permission => <li>{permission}</li>}</For></ul></Show>
  </>;
}
