import { Show, createSignal } from "solid-js";

import { createManagedRole, deleteManagedRole, updateManagedRole } from "@/entities/administration/api";
import { hasPermission, sessionAuthorizationKey } from "@/entities/session/store";
import type { ManagedRole, PolicyVersion } from "@/gen/portcullis/v1/administration_pb";
import { ConfirmManagementAction, ManagementFeedback, ManagementField, ManagementIDReceipt, createManagementAction } from "@/features/administration/controls";
import { RolePermissionCatalog } from "@/features/administration/RolePermissionCatalog";
import { Button } from "@/shared/ui/button";

/** Edits a reusable bundle without conflating a Role with its recipients. */
export function RoleForm(props: { role?: ManagedRole; onAdmissionFailure: () => void }) {
  const [name, setName] = createSignal(props.role?.summary?.name ?? "");
  const [permissions, setPermissions] = createSignal(props.role?.permissions.join("\n") ?? "");
  const [receipt, setReceipt] = createSignal<PolicyVersion>();
  const [committed, setCommitted] = createSignal(false);
  const action = createManagementAction(props.onAdmissionFailure);
  return <form class="flex max-w-2xl flex-col gap-4" onSubmit={event => { event.preventDefault();
    const input = { name: name(), permissions: permissions().split(/[\s,]+/).filter(Boolean) };
    void action.run(async () => props.role?.summary ? updateManagedRole({ ...input, id: props.role.summary.id, expectedVersion: props.role.summary.version }) : createManagedRole(input), props.role ? "Saved role. Reload to obtain the current version." : "Created role.").then(result => { if (result) { setCommitted(true); setReceipt(result.value.role); } });
  }}><fieldset class="flex flex-col gap-4" disabled={action.busy() || action.locked() || committed()}><ManagementField label="Role name" value={name()} onInput={setName} required /><label class="flex flex-col gap-1 text-sm">Permission keys<textarea class="min-h-32 w-full rounded border border-input bg-background p-2 font-mono" value={permissions()} onInput={event => setPermissions(event.currentTarget.value)} /></label><p class="text-sm text-muted-foreground">One catalog key per line. Empty roles grant nothing. System roles are read only; the server checks all old and new permissions and live Binding recipients.</p><Button type="submit">{props.role ? "Save role" : "Create role"}</Button></fieldset>
    <ManagementFeedback error={action.error()} message={action.message()} /><Show when={receipt()}>{version => <ManagementIDReceipt label="Role ID receipt" id={version().id} version={version().version} />}</Show>
    <Show when={hasPermission("roles.list")}><Show when={sessionAuthorizationKey()} keyed>{_authorization => <RolePermissionCatalog onAdmissionFailure={props.onAdmissionFailure} />}</Show></Show>
  </form>;
}

/** Removes only an unreferenced custom role; the server retains final authority. */
export function DeleteRoleAction(props: { id: string; version: bigint; isSystem: boolean; bindingCount: bigint; onAdmissionFailure: () => void }) {
  const action = createManagementAction(props.onAdmissionFailure); const [deleted, setDeleted] = createSignal(false);
  return <Show when={hasPermission("roles.delete") && !props.isSystem}><ConfirmManagementAction label="Delete role" description="Delete this custom permission definition. Live Bindings must be revoked first; the server protects the last administrator." disabled={props.bindingCount > 0n || deleted() || action.busy() || action.locked()} onConfirm={async () => { const result = await action.run(() => deleteManagedRole(props.id, props.version), "Deleted role. Refresh the list."); if (result) setDeleted(true); }} /><ManagementFeedback error={action.error()} message={action.message()} /></Show>;
}
