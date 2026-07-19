import type { Component } from "solid-js";
import { Match, Show, Switch } from "solid-js";

import type { RouteSectionProps } from "@solidjs/router";
import { Navigate } from "@solidjs/router";

import { UnreachableCard } from "@/entities/session/UnreachableCard";
import { can, session } from "@/entities/session/store";
import { LogoutButton } from "@/features/auth/LogoutButton";

// AppShell is the authenticated layout: ONE route guard for every signed-in
// page (anonymous → login, unreachable → retry card) plus the app header. The
// header keeps navigation (left) and the user area (right) as separate
// regions — account info and sign-out never mix into the nav. Composition
// root concern (frontend.md): only the app layer may compose entities
// (session) with features (auth) like this.
//
// Nav items are permission-gated via can() — pure affordance hiding; the
// server still returns the uniform permission-denied if an RPC is attempted
// (ADR-0008). Future sections (Requests, Audit) append here the same way.
const AppShell: Component<RouteSectionProps> = (props) => (
  <Switch>
    <Match when={session().status === "anonymous"}>
      <Navigate href="/login" />
    </Match>
    <Match when={session().status === "unreachable"}>
      <UnreachableCard />
    </Match>
    <Match when={session().status === "authenticated"}>
      <div class="flex min-h-screen flex-col">
        <header class="border-b">
          <div class="mx-auto flex w-full max-w-4xl items-center justify-between gap-6 px-6 py-3">
            <div class="flex items-center gap-6">
              <span class="text-base font-semibold tracking-tight">Portcullis</span>
              <nav aria-label="Main" class="flex items-center gap-4">
                <Show when={can("connections.list")}>
                  <a
                    class="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
                    href="/connections"
                  >
                    Connections
                  </a>
                </Show>
              </nav>
            </div>
            <div class="flex items-center gap-3">
              <span class="text-sm text-muted-foreground">
                {(() => {
                  const s = session();
                  return s.status === "authenticated" ? s.user.email : "";
                })()}
              </span>
              <LogoutButton />
            </div>
          </div>
        </header>
        <main class="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-6 p-6">{props.children}</main>
      </div>
    </Match>
  </Switch>
);

export default AppShell;
