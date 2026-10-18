import { Show, createSignal, onCleanup } from "solid-js";

import { createManagedBinding, revokeManagedBinding, updateManagedBinding, validateManagedCondition } from "@/entities/administration/api";
import { buildCondition, isManagementID, type ConditionChoices } from "@/entities/administration/condition";
import { hasPermission } from "@/entities/session/store";
import type { ManagedBinding, PolicyVersion } from "@/gen/portcullis/v1/administration_pb";
import { ConditionFields } from "@/features/administration/ConditionFields";
import { ConfirmManagementAction, ManagementFeedback, ManagementField, ManagementIDReceipt, ManagementSelect, createManagementAction } from "@/features/administration/controls";
import { Button } from "@/shared/ui/button";

/** Edits structured grants while preserving every existing expression until an explicit change. */
export function BindingForm(props: { binding?: ManagedBinding; onAdmissionFailure: () => void }) {
  const head = props.binding?.summary;
  const [userID, setUserID] = createSignal(head?.userId ?? "");
  const [roleID, setRoleID] = createSignal(head?.roleId ?? "");
  const [scope, setScope] = createSignal(head?.scope?.type ?? "organization");
  const [connectionID, setConnectionID] = createSignal(head?.scope?.connectionId ?? "");
  const [mode, setMode] = createSignal(head ? "preserve" : "none");
  const [choices, setChoices] = createSignal<ConditionChoices>({ title: "", startsAt: "", expiresAt: "", resourceType: "", resourceID: "" });
  const [receipt, setReceipt] = createSignal<PolicyVersion>();
  const [committed, setCommitted] = createSignal(false); const [inputError, setInputError] = createSignal("");
  const action = createManagementAction(props.onAdmissionFailure);
  const [validation, setValidation] = createSignal("");
  onCleanup(() => setValidation(""));
  /** Validates the exact generated source and fences the write after asynchronous preparation. */
  async function saveBinding() {
    setInputError(""); setValidation("");
    if (!isManagementID(userID()) || !isManagementID(roleID()) || (scope() === "connection" && !isManagementID(connectionID()))) { setInputError("Use canonical UUIDs for the user, role and connection."); return; }
    let condition: ReturnType<typeof buildCondition> | undefined;
    if (mode() === "replace") {
      try { condition = buildCondition(choices()); }
      catch { setInputError("Select a title and valid time or resource restrictions. No grant was sent."); return; }
    }
    const capturedMode = mode();
    const selectedScope = { type: scope(), connectionId: scope() === "connection" ? connectionID() : "" };
    const result = await action.run(async isCurrent => {
      if (condition) {
        await validateManagedCondition(condition);
        if (!isCurrent()) throw new Error("Route changed during validation");
      }
      if (!head) return createManagedBinding({ userId: userID(), roleId: roleID(), scope: selectedScope, condition });
      const conditionEdit = capturedMode === "replace" && condition ? { case: "replaceCondition", value: condition } as const : capturedMode === "clear" ? { case: "clearCondition", value: true } as const : undefined;
      return updateManagedBinding({ id: head.id, expectedVersion: head.version, roleId: roleID(), scope: selectedScope, conditionEdit });
    }, head ? "Saved binding. Reload to obtain its current version." : "Created binding.");
    if (result) { setCommitted(true); setReceipt(result.value.binding); setValidation(condition ? "The server validated the generated condition before saving." : ""); }
  }
  return <form class="flex max-w-2xl flex-col gap-4" onSubmit={event => { event.preventDefault(); if (mode() !== "clear") void saveBinding(); }}>
    <fieldset class="flex flex-col gap-4" disabled={action.busy() || action.locked() || committed()}>
      <ManagementField label="User ID" value={userID()} onInput={setUserID} required disabled={head !== undefined} />
      <ManagementField label="Role ID" value={roleID()} onInput={setRoleID} required />
      <p class="text-sm text-muted-foreground">Use IDs from authorized Users and Roles lists. Listing those resources requires separate permissions.</p>
      <ManagementSelect label="Scope" value={scope()} onChange={setScope}><option value="organization">Organization</option><option value="connection">Specific connection</option></ManagementSelect>
      <Show when={scope() === "connection"}><ManagementField label="Connection ID" value={connectionID()} onInput={setConnectionID} required /></Show>
      <ManagementSelect label="Condition change" value={mode()} onChange={setMode}>
        <Show when={head} fallback={<option value="none">No condition</option>}><option value="preserve">Preserve stored condition</option><option value="clear">Remove condition explicitly</option></Show>
        <option value="replace">{head ? "Replace with form restrictions" : "Add form restrictions"}</option>
      </ManagementSelect>
      <Show when={mode() === "replace"}><ConditionFields value={choices()} onChange={setChoices} /></Show>
      <p class="text-sm text-muted-foreground">No condition grants access whenever the Role and scope match. Grants add together; a conditional Binding never restricts another broader Binding.</p>
      <Show when={mode() === "clear"} fallback={<Button type="submit">{head ? "Save binding" : "Create binding"}</Button>}><ConfirmManagementAction label="Remove condition and save" description="This broadens the grant by removing its condition. The server checks all affected permissions and the last administrator before saving." onConfirm={saveBinding} /></Show>
    </fieldset><ManagementFeedback error={inputError() || action.error()} message={action.message()} /><Show when={receipt()}>{version => <ManagementIDReceipt label="Binding ID receipt" id={version().id} version={version().version} />}</Show><Show when={validation()}><p class="text-sm">{validation()}</p></Show>
  </form>;
}

/** Revokes a grant from its summary without fetching condition source or allowing self changes. */
export function RevokeBindingAction(props: { id: string; version: bigint; own: boolean; revoked: boolean; onAdmissionFailure: () => void }) {
  const action = createManagementAction(props.onAdmissionFailure); const [revoked, setRevoked] = createSignal(false);
  return <Show when={!props.own && !props.revoked && hasPermission("bindings.delete")}><ConfirmManagementAction label="Revoke binding" description="Revoke this grant and its subject's sessions. The server rechecks complete delegation and protects the last recovery administrator." disabled={revoked() || action.busy() || action.locked()} onConfirm={async () => { const result = await action.run(() => revokeManagedBinding(props.id, props.version), "Revoked binding. Refresh to see its current state."); if (result) setRevoked(true); }} /><ManagementFeedback error={action.error()} message={action.message()} /></Show>;
}
