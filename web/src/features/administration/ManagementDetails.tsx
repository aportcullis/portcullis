import { Show, createEffect, createSignal, onCleanup } from "solid-js";

import { getManagement, type ManagementDetail, type ManagementKind } from "@/entities/administration/api";
import { createManagementQuery } from "@/entities/administration/query";
import { hasPermission, load } from "@/entities/session/store";
import type { UserSetupLink } from "@/gen/portcullis/v1/administration_pb";
import { BindingForm, RevokeBindingAction } from "@/features/administration/BindingManagement";
import { DeleteRoleAction, RoleForm } from "@/features/administration/RoleManagement";
import { UserSetupAction, UserStatusAction } from "@/features/administration/UserManagement";
import { ManagementFeedback } from "@/features/administration/controls";
import { Button } from "@/shared/ui/button";
import { LoadingSkeleton } from "@/shared/ui/LoadingSkeleton";

/** Loads protected detail independently, with explicit reload discarding stale forms. */
export function ManagementDetails(props: { kind: ManagementKind; id: string; actorID: string; onSetupLink: (link: UserSetupLink | undefined) => void; onAdmissionFailure: () => void }) {
  const [accountChanged, setAccountChanged] = createSignal(false);
  const query = createManagementQuery<ManagementDetail>(() => void load(), () => props.onSetupLink(undefined));
  const reload = () => { setAccountChanged(false); return query.load(() => getManagement(props.kind, props.id)); };
  createEffect(() => { void reload(); }); onCleanup(query.reset);
  return <section class="flex flex-col gap-4"><Button class="self-start" variant="outline" onClick={() => void reload()}>Reload current version</Button><ManagementFeedback error={query.error()} /><Show when={query.loading()}><LoadingSkeleton label="Loading authorized details…" /></Show>
    <Show when={query.data()} keyed>{detail => <>
      <Show when={detail.kind === "users" ? detail.value : undefined}>{account => <>
        <p>{account().user?.displayName} · {account().user?.email} · {account().user?.status}</p><p class="break-all font-mono">{account().user?.id}</p><p>Password {account().hasPassword ? "configured" : "not configured"}</p>
        <UserStatusAction id={props.id} status={account().user?.status ?? ""} own={props.id === props.actorID} onChanged={() => { props.onSetupLink(undefined); setAccountChanged(true); }} onAdmissionFailure={props.onAdmissionFailure} />
        <UserSetupAction id={props.id} active={account().user?.status === "active"} hasPassword={account().hasPassword} stale={accountChanged()} own={props.id === props.actorID} onSetupLink={props.onSetupLink} onAdmissionFailure={props.onAdmissionFailure} />
      </>}</Show>
      <Show when={detail.kind === "roles" ? detail.value : undefined}>{role => <>
        <p>Version {role().summary?.version.toString()} · {role().summary?.bindingCount.toString()} live Bindings</p>
        <Show when={!role().summary?.isSystem && hasPermission("roles.update")} fallback={<><h2>{role().summary?.name}</h2><p>{role().summary?.isSystem ? "System role — read only" : "Read only"}</p><pre class="max-w-full whitespace-pre-wrap break-all">{role().permissions.join("\n") || "No permissions"}</pre></>}><RoleForm role={role()} onAdmissionFailure={props.onAdmissionFailure} /></Show>
        <DeleteRoleAction id={props.id} version={role().summary?.version ?? 0n} isSystem={role().summary?.isSystem ?? true} bindingCount={role().summary?.bindingCount ?? 0n} onAdmissionFailure={props.onAdmissionFailure} />
      </>}</Show>
      <Show when={detail.kind === "bindings" ? detail.value : undefined}>{binding => <>
        <p class="break-all">User: {binding().summary?.userId} · Role: {binding().summary?.roleId}</p>
        <p class="break-all">Scope: {binding().summary?.scope?.type} {binding().summary?.scope?.connectionId} · Version {binding().summary?.version.toString()} · {binding().summary?.revoked ? "Revoked" : "Live"}</p>
        <Show when={binding().condition} fallback={<p>No condition</p>}>{source => <section aria-label="Stored condition"><h2>{source().title}</h2><p>{source().description}</p><p>{source().languageVersion} / {source().environmentVersion}</p><pre class="max-w-full whitespace-pre-wrap break-all rounded border p-3 font-mono text-sm">{source().expression}</pre><p class="text-sm text-muted-foreground">The form does not reinterpret stored source. Preserve, explicitly replace or explicitly remove it.</p></section>}</Show>
        <Show when={binding().condition && (binding().condition?.languageVersion !== "cel-v1" || binding().condition?.environmentVersion !== "portcullis-v1")}><p>Unsupported condition contract — inspection and permitted revocation only.</p></Show>
        <Show when={hasPermission("bindings.update") && binding().summary?.userId !== props.actorID && !binding().summary?.revoked && (!binding().condition || (binding().condition?.languageVersion === "cel-v1" && binding().condition?.environmentVersion === "portcullis-v1"))}><BindingForm binding={binding()} onAdmissionFailure={props.onAdmissionFailure} /></Show>
        <RevokeBindingAction id={props.id} version={binding().summary?.version ?? 0n} own={binding().summary?.userId === props.actorID} revoked={binding().summary?.revoked ?? true} onAdmissionFailure={props.onAdmissionFailure} />
      </>}</Show>
    </>}</Show>
  </section>;
}
