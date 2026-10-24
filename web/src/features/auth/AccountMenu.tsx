import type { Component, JSX } from "solid-js";
import { For, Show, createEffect, createSignal, on, onCleanup, splitProps } from "solid-js";

import type { SessionState } from "@/entities/session/store";
import { logout, session, sessionInteractionKey } from "@/entities/session/store";
import type { SessionBinding } from "@/gen/portcullis/v1/auth_pb";
import { Avatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";

/** Supplies the current route without moving application navigation into the account feature. */
export type AccountMenuProps = {
  routeKey: string;
};

type AuthenticatedAccount = Extract<SessionState, { status: "authenticated" }>;
type AccountSignOutState = { kind: "idle" } | { kind: "pending" } | { kind: "failed"; message: string };
type AccountTriggerReference = { kind: "absent" } | { kind: "ready"; element: HTMLButtonElement };
type AccountDisclosureProps = AccountMenuProps;

/** Reveals current account details within one authenticated interaction lifetime. */
export const AccountMenu: Component<AccountMenuProps> = props => {
  const [menuProps] = splitProps(props, ["routeKey"]);
  return (
    <Show when={sessionInteractionKey()} keyed>
      {_interaction => <AccountDisclosure routeKey={menuProps.routeKey} />}
    </Show>
  );
};

/** Keeps account action state alive while its disclosure closes or the route changes. */
const AccountDisclosure: Component<AccountDisclosureProps> = props => {
  const [menuProps] = splitProps(props, ["routeKey"]);
  const [open, setOpen] = createSignal(false);
  const [signOutState, setSignOutState] = createSignal<AccountSignOutState>({ kind: "idle" });
  let disposed = false;
  let tabIntention = false;
  let nativeFocusExit = false;
  let accountTrigger: AccountTriggerReference = { kind: "absent" };

  const readAuthenticatedAccount = (): AuthenticatedAccount | false => {
    const principal = session();
    return isAuthenticatedAccount(principal) ? principal : false;
  };
  const readSignOutFailureMessage = (): string => {
    const state = signOutState();
    return state.kind === "failed" ? state.message : "";
  };
  const changeAccountDisclosureOpen = (nextOpen: boolean): void => {
    if (disposed) return;
    if (nextOpen) {
      tabIntention = false;
      nativeFocusExit = false;
    }
    setOpen(nextOpen);
  };
  const recordAccountTabIntention: JSX.EventHandler<HTMLDivElement, KeyboardEvent> = event => {
    tabIntention = event.key === "Tab";
  };
  const closeAccountAfterNativeFocusExit: JSX.EventHandler<HTMLDivElement, FocusEvent> = event => {
    if (!tabIntention) return;
    tabIntention = false;
    const { currentTarget, relatedTarget } = event;
    assertAccountRegion(currentTarget);
    if (isAccountNode(relatedTarget) && currentTarget.contains(relatedTarget)) return;
    nativeFocusExit = true;
    setOpen(false);
  };
  const preserveAccountDismissalFocus = (event: Event): void => {
    if (nativeFocusExit || disposed || open()) event.preventDefault();
  };
  const captureAccountTrigger = (value: unknown): void => {
    assertAccountTrigger(value);
    accountTrigger = { kind: "ready", element: value };
  };
  const focusAccountTriggerOnEscape = (): void => {
    if (disposed || !open()) return;
    const currentTrigger = accountTrigger;
    if (currentTrigger.kind === "ready" && currentTrigger.element.isConnected) {
      currentTrigger.element.focus({ preventScroll: true });
    }
  };
  const signOutAccount = async (): Promise<void> => {
    if (disposed || signOutState().kind === "pending") return;
    setSignOutState({ kind: "pending" });
    try {
      await logout();
      if (!disposed) setSignOutState({ kind: "idle" });
    } catch {
      if (!disposed) {
        setSignOutState({
          kind: "failed",
          message: "Sign out failed — check your connection and try again.",
        });
      }
    }
  };

  createEffect(on(() => menuProps.routeKey, () => setOpen(false)));
  onCleanup(() => {
    disposed = true;
    accountTrigger = { kind: "absent" };
  });

  return (
    <Show when={readAuthenticatedAccount()}>
      {account => (
        <Popover
          open={open()}
          onOpenChange={changeAccountDisclosureOpen}
          modal={false}
          placement="bottom-end"
          gutter={8}
          overflowPadding={16}
          fitViewport
        >
          <PopoverTrigger
            ref={captureAccountTrigger}
            type="button"
            aria-label="Account menu"
            aria-haspopup={false}
            class="inline-flex size-10 shrink-0 items-center justify-center rounded-lg hover:bg-accent"
          >
            <Avatar seed={account().user.id} label="Your profile image" />
          </PopoverTrigger>
          <PopoverContent
            role="region"
            aria-label="Account details"
            onKeyDown={recordAccountTabIntention}
            onFocusOut={closeAccountAfterNativeFocusExit}
            onCloseAutoFocus={preserveAccountDismissalFocus}
            onEscapeKeyDown={focusAccountTriggerOnEscape}
            class="flex max-h-[calc(100dvh-2rem)] flex-col gap-4 overflow-y-auto"
          >
            <div class="min-w-0">
              <p class="break-all font-semibold">{selectAccountDisplayName(account())}</p>
              <Show when={account().user.displayName.length > 0}>
                <p class="mt-1 break-all text-sm text-muted-foreground">{account().user.email}</p>
              </Show>
            </div>
            <div class="min-w-0">
              <p class="mb-2 text-xs font-medium text-muted-foreground">Current access</p>
              <Show when={account().bindings.length > 0} fallback={<p class="text-sm text-muted-foreground">No active role bindings.</p>}>
                <ul aria-label="Current role bindings" class="flex max-h-60 flex-col gap-3 overflow-y-auto">
                  <For each={account().bindings}>
                    {binding => (
                      <li class="min-w-0 text-sm">
                        <p class="break-all font-medium">{binding.roleName}</p>
                        <p class="break-all text-xs text-muted-foreground">
                          {describeAccountBindingScope(binding)}
                        </p>
                        <Show when={binding.hasCondition}>
                          <p class="text-xs text-muted-foreground">Conditional</p>
                        </Show>
                      </li>
                    )}
                  </For>
                </ul>
              </Show>
            </div>
            <Button
              type="button"
              variant="outline"
              class="w-full"
              disabled={signOutState().kind === "pending"}
              aria-busy={signOutState().kind === "pending"}
              onClick={() => void signOutAccount()}
            >
              Sign out
            </Button>
            <Show when={readSignOutFailureMessage()}>
              <p role="alert" class="break-words text-sm text-destructive-text">{readSignOutFailureMessage()}</p>
            </Show>
          </PopoverContent>
        </Popover>
      )}
    </Show>
  );
};

/** Narrows the existing session contract before rendering account data. */
function isAuthenticatedAccount(principal: SessionState): principal is AuthenticatedAccount {
  return principal.status === "authenticated";
}

/** Selects the required display name or its explicit legacy email fallback. */
function selectAccountDisplayName(account: AuthenticatedAccount): string {
  const { user } = account;
  const { displayName, email } = user;
  return displayName.length > 0 ? displayName : email;
}

/** Describes only recognized Binding scopes with an available exact target. */
function describeAccountBindingScope(binding: SessionBinding): string {
  const { scopeType, connectionId } = binding;
  if (scopeType === "organization") return "Organization";
  if (scopeType === "connection" && connectionId.length > 0) return `Connection: ${connectionId}`;
  return "Scope unavailable";
}

/** Narrows focus destinations without treating an absent browser target as an element. */
function isAccountNode(value: unknown): value is Node {
  return value instanceof Node;
}

/** Requires the native region element before checking its focus boundary. */
function assertAccountRegion(value: unknown): asserts value is HTMLDivElement {
  if (!(value instanceof HTMLDivElement)) throw new Error("The account details region is unavailable.");
}

/** Requires the primitive's native trigger before retaining its focus target. */
function assertAccountTrigger(value: unknown): asserts value is HTMLButtonElement {
  if (!(value instanceof HTMLButtonElement)) throw new Error("The account trigger is unavailable.");
}
