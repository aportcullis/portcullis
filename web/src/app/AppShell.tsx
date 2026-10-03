import type { Component } from "solid-js";
import { For, Match, Show, Switch } from "solid-js";

import type { RouteSectionProps } from "@solidjs/router";
import { Navigate } from "@solidjs/router";

import { visibleSections } from "@/app/navigation";
import { createPendingRequests } from "@/app/pendingRequests";
import { UnreachableCard } from "@/entities/session/UnreachableCard";
import { hasPermission, session } from "@/entities/session/store";
import { LogoutButton } from "@/features/auth/LogoutButton";
import { BrandLogo } from "@/shared/ui/BrandLogo";
import { Badge } from "@/shared/ui/badge";

// AppShell owns the session guard and capability-based navigation; the server still authorizes every RPC.
const AppShell: Component<RouteSectionProps> = (props) => {
  const pendingCount = createPendingRequests();
  return (
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
                <BrandLogo class="h-8" />
                <nav aria-label="Main" class="flex items-center gap-4">
                  <For each={visibleSections(hasPermission)}>
                    {(section) => (
                      <a
                        class="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
                        href={section.href}
                      >
                        {section.label}
                        <Show when={section.href === "/requests" && pendingCount() > 0n}><Badge class="ml-1" variant="secondary">{pendingCount().toString()} pending</Badge></Show>
                      </a>
                    )}
                  </For>
                </nav>
              </div>
              <div class="flex items-center gap-3">
                <span class="text-sm text-muted-foreground">
                  {(() => {
                    const s = session();
                    if (s.status !== "authenticated") return "";
                    return s.user.displayName || s.user.email;
                  })()}
                </span>
                {/* Role badge: the membership's display label (empty when the server degraded resolution) — a label, never authorization. */}
                <Show
                  when={(() => {
                    const s = session();
                    return s.status === "authenticated" && s.roleName !== "" ? s.roleName : "";
                  })()}
                >
                  {(role) => <Badge variant="secondary">{role()}</Badge>}
                </Show>
                <LogoutButton />
              </div>
            </div>
          </header>
          <main class="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-6 p-6">{props.children}</main>
        </div>
      </Match>
    </Switch>
  );
};

export default AppShell;
