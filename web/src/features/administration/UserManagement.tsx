import { Show, createEffect, createSignal } from "solid-js";

import {
  changeManagedUserStatus,
  createManagedUser,
  issueManagedSetup,
} from "@/entities/administration/api";
import { hasPermission } from "@/entities/session/store";
import type { UserSetupLink } from "@/gen/portcullis/v1/administration_pb";
import {
  ConfirmManagementAction,
  ManagementFeedback,
  ManagementField,
  ManagementIDReceipt,
  createManagementAction,
} from "@/features/administration/controls";
import { Button } from "@/shared/ui/button";
import { nativeControlVariants } from "@/shared/ui/control";
import { passwordSetupLink } from "@/shared/lib/passwordSetupLink";

/** Presents a fresh token only in volatile route state with explicit dismissal. */
export function SetupTokenReceipt(props: { link: UserSetupLink; onDismiss: () => void }) {
  return (
    <section aria-label="One-time setup delivery" class="flex flex-col gap-3 rounded-md border p-4">
      <p>
        Deliver this setup link privately to the account owner. It is shown only for this issuance.
      </p>
      <label class="flex flex-col gap-1">
        Setup link
        <input
          class={nativeControlVariants({ class: "w-full font-mono" })}
          readOnly
          value={passwordSetupLink(window.location.origin, props.link.token)}
        />
      </label>
      <label class="flex flex-col gap-1">
        Setup token
        <input
          class={nativeControlVariants({ class: "w-full font-mono" })}
          readOnly
          value={props.link.token}
        />
      </label>
      <Show when={props.link.expiresAt}>
        {(expiry) => <p>Expires: {new Date(Number(expiry().seconds) * 1000).toISOString()}</p>}
      </Show>
      <Button variant="outline" onClick={props.onDismiss}>
        Dismiss setup token
      </Button>
    </section>
  );
}

/** Creates independent membership without assigning an implicit Role. */
export function CreateUserForm(props: { onAdmissionFailure: () => void }) {
  const [createdID, setCreatedID] = createSignal("");
  const [email, setEmail] = createSignal("");
  const [name, setName] = createSignal("");
  const [link, setLink] = createSignal<UserSetupLink>();
  const [created, setCreated] = createSignal(false);
  const action = createManagementAction(() => {
    setLink(undefined);
    props.onAdmissionFailure();
  });
  /** Commits one explicit creation intent and records only its current receipt. */
  async function submitUserForm(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const result = await action.run(
      () => createManagedUser({ email: email(), displayName: name() }),
      "Created user without grants. Add a separate Binding to grant access.",
    );
    if (!result) return;

    setCreated(true);
    setCreatedID(result.value.user?.id ?? "");
    setLink(result.value.setupLink);
  }

  return (
    <form class="flex max-w-2xl flex-col gap-4" onSubmit={(event) => void submitUserForm(event)}>
      <fieldset
        disabled={action.busy() || action.locked() || created()}
        class="flex flex-col gap-4"
      >
        <ManagementField label="Email" type="email" value={email()} onInput={setEmail} required />
        <ManagementField label="Display name" value={name()} onInput={setName} required />
        <p class="text-sm text-muted-foreground">
          Membership does not grant permissions. Create Bindings separately for this user.
        </p>
        <Button type="submit">Create user</Button>
      </fieldset>
      <ManagementFeedback error={action.error()} message={action.message()} />
      <Show when={createdID()}>
        {(identifier) => <ManagementIDReceipt label="Created user ID" id={identifier()} />}
      </Show>
      <Show when={link()}>
        {(receipt) => <SetupTokenReceipt link={receipt()} onDismiss={() => setLink(undefined)} />}
      </Show>
    </form>
  );
}

/** Changes account status independently of detail permissions and protects self controls. */
export function UserStatusAction(props: {
  id: string;
  status: string;
  own: boolean;
  onChanged?: () => void;
  onAdmissionFailure: () => void;
}) {
  const action = createManagementAction(props.onAdmissionFailure);
  const [changed, setChanged] = createSignal(false);
  const enable = () => props.status === "disabled";
  /** Applies the status change only when the mutation still belongs to this interaction. */
  async function changeUserStatus(): Promise<void> {
    const result = await action.run(
      () => changeManagedUserStatus(props.id, enable()),
      "Saved account state. Refresh to see the current account.",
    );
    if (!result) return;

    setChanged(true);
    props.onChanged?.();
  }

  return (
    <Show when={!props.own && hasPermission("users.disable")}>
      <ConfirmManagementAction
        label={enable() ? "Enable user" : "Disable user"}
        description="The server rechecks complete delegation and protects the last recovery administrator. Account changes revoke affected sessions immediately."
        disabled={changed() || action.busy() || action.locked()}
        onConfirm={changeUserStatus}
      />
      <ManagementFeedback error={action.error()} message={action.message()} />
    </Show>
  );
}

/** Reissues setup only after an authorized passwordless account detail read. */
export function UserSetupAction(props: {
  id: string;
  active: boolean;
  hasPassword: boolean;
  own: boolean;
  stale?: boolean;
  onSetupLink: (link: UserSetupLink | undefined) => void;
  onAdmissionFailure: () => void;
}) {
  const action = createManagementAction(() => {
    props.onSetupLink(undefined);
    props.onAdmissionFailure();
  });
  const shouldDiscardDelivery = () =>
    props.stale || !props.active || props.hasPassword || props.own;
  const mayIssueSetup = () =>
    !props.stale &&
    !props.own &&
    props.active &&
    !props.hasPassword &&
    hasPermission("users.update");

  createEffect(() => {
    if (shouldDiscardDelivery()) props.onSetupLink(undefined);
  });

  /** Clears earlier delivery before issuing a replacement under the current action lifetime. */
  async function issueSetupToken(): Promise<void> {
    props.onSetupLink(undefined);
    const result = await action.run(
      () => issueManagedSetup(props.id),
      "Issued a replacement setup token.",
    );
    if (result) props.onSetupLink(result.value.setupLink);
  }

  return (
    <Show when={mayIssueSetup()}>
      <Button
        variant="outline"
        disabled={action.busy() || action.locked()}
        onClick={() => void issueSetupToken()}
      >
        Issue setup token
      </Button>
      <ManagementFeedback error={action.error()} message={action.message()} />
    </Show>
  );
}
