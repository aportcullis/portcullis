import { Match, Show, Switch, createMemo, createSignal } from "solid-js";
import { useParams } from "@solidjs/router";

import type { ManagementKind } from "@/entities/administration/api";
import { isManagementID } from "@/entities/administration/condition";
import { hasPermission, session, sessionAuthorizationKey, sessionInteractionKey } from "@/entities/session/store";
import { SessionInteraction } from "@/entities/session/SessionInteraction";
import type { UserSetupLink } from "@/gen/portcullis/v1/administration_pb";
import { BindingForm } from "@/features/administration/BindingManagement";
import { ManagementCollection } from "@/features/administration/ManagementCollection";
import { ManagementDetails } from "@/features/administration/ManagementDetails";
import { RoleForm } from "@/features/administration/RoleManagement";
import { CreateUserForm, SetupTokenReceipt } from "@/features/administration/UserManagement";
import { PageHeader } from "@/shared/ui/PageHeader";

/** Owns local creation inputs and delivery independently of revision-owned protected reads. */
export function ManagementWorkspace(props: { kind: ManagementKind }) {
  const params = useParams();
  const owner = createMemo(() => { const interaction = sessionInteractionKey(); return interaction ? JSON.stringify([interaction, props.kind, params.id ?? ""]) : ""; });
  return <Show when={owner()} keyed>{_owner => <SessionInteraction><ManagementSession kind={props.kind} id={params.id ?? ""} /></SessionInteraction>}</Show>;
}

/** Composes independent read/create affordances while every RPC retains server authority. */
function ManagementSession(props: { kind: ManagementKind; id: string }) {
  const [denied, setDenied] = createSignal(false);
  const [setupLink, setSetupLink] = createSignal<UserSetupLink>();
  const principal = session();
  const actorID = principal.status === "authenticated" ? principal.user.id : "";
  const title = props.kind === "users" ? "Users" : props.kind === "roles" ? "Roles" : "Bindings";
  const singular = props.kind === "users" ? "user" : props.kind === "roles" ? "role" : "binding";
  const isNew = props.id === "new";
  const onAdmissionFailure = () => { setSetupLink(undefined); setDenied(true); };
  return <div class="flex min-w-0 flex-col gap-5">
    <PageHeader eyebrow="Access administration" title={isNew ? `Create ${singular}` : props.id ? `${singular[0].toUpperCase()}${singular.slice(1)} details` : title} description={props.kind === "users" ? "Manage independent organization membership and account state." : props.kind === "roles" ? "Define reusable permission bundles. Grant them through separate Bindings." : "Grant Roles to users at organization or connection scope, with optional conditions."} />
    <Show when={props.id} fallback={<Show when={hasPermission(`${props.kind}.create`)}><a class="self-start text-sm underline" href={`/${props.kind}/new`}>Create {singular}</a></Show>}><a class="self-start text-sm underline" href={`/${props.kind}`}>Back to {title}</a></Show>
    <Show when={!denied()} fallback={<p role="alert">Permission changed. Protected data and drafts were cleared. Reload to check your current access.</p>}>
      <Switch>
        <Match when={isNew && hasPermission(`${props.kind}.create`)}>
          <Switch><Match when={props.kind === "users"}><CreateUserForm onAdmissionFailure={onAdmissionFailure} /></Match><Match when={props.kind === "roles"}><RoleForm onAdmissionFailure={onAdmissionFailure} /></Match><Match when={props.kind === "bindings"}><BindingForm onAdmissionFailure={onAdmissionFailure} /></Match></Switch>
        </Match>
        <Match when={props.id !== "" && !isNew && isManagementID(props.id) && hasPermission(`${props.kind}.get`)}><Show when={sessionAuthorizationKey()} keyed>{_authorization => <ManagementDetails kind={props.kind} id={props.id} actorID={actorID} onSetupLink={setSetupLink} onAdmissionFailure={onAdmissionFailure} />}</Show></Match>
        <Match when={props.id === "" && hasPermission(`${props.kind}.list`)}><Show when={sessionAuthorizationKey()} keyed>{_authorization => <ManagementCollection kind={props.kind} actorID={actorID} onAdmissionFailure={onAdmissionFailure} />}</Show></Match>
        <Match when={true}><p role="alert">This page requires separate {props.kind}.{isNew ? "create" : props.id ? "get" : "list"} permission and a valid resource identifier.</p></Match>
      </Switch>
      <Show when={setupLink()}>{receipt => <SetupTokenReceipt link={receipt()} onDismiss={() => setSetupLink(undefined)} />}</Show>
    </Show>
  </div>;
}
