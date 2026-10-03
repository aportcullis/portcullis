import type { Component } from "solid-js";
import { For, Match, Show, Switch } from "solid-js";

import type { RouteSectionProps } from "@solidjs/router";
import { Navigate, useLocation } from "@solidjs/router";

import { visibleSections } from "@/app/navigation";
import { createPendingRequests } from "@/app/pendingRequests";
import { UnreachableCard } from "@/entities/session/UnreachableCard";
import { hasPermission, session } from "@/entities/session/store";
import { LogoutButton } from "@/features/auth/LogoutButton";
import { ApplicationFrame } from "@/shared/ui/ApplicationFrame";
import { BrandLogo } from "@/shared/ui/BrandLogo";
import { Badge } from "@/shared/ui/badge";
import { Avatar } from "@/shared/ui/avatar";

// AppShell owns the session guard and capability-based navigation; the server still authorizes every RPC.
const AppShell: Component<RouteSectionProps> = (props) => {
  const pendingCount = createPendingRequests();
  const location = useLocation();
  const currentUser = () => {
    const current = session();
    return current.status === "authenticated" ? current.user : undefined;
  };
  return (
    <Switch>
      <Match when={session().status === "anonymous"}>
        <Navigate href="/login" />
      </Match>
      <Match when={session().status === "unreachable"}>
        <UnreachableCard />
      </Match>
      <Match when={session().status === "authenticated"}>
        <ApplicationFrame
          brand={<BrandLogo class="h-8" />}
          navigation={
            <nav aria-label="Main" class="flex items-center gap-4">
              <For each={visibleSections(hasPermission)}>
                {(section) => (
                  <a
                    aria-current={location.pathname === section.href || location.pathname.startsWith(`${section.href}/`) ? "page" : undefined}
                    href={section.href}
                  >
                    {section.label}
                    <Show when={section.href === "/requests" && pendingCount() > 0n}><Badge class="ml-1" variant="secondary">{pendingCount().toString()} pending</Badge></Show>
                  </a>
                )}
              </For>
            </nav>
          }
          account={<>
            <Show when={currentUser()}>
              {(user) => <div class="flex items-center gap-2">
                <Avatar seed={user().id} label={`Profile image for ${user().displayName || user().email}`} />
                <span class="text-sm text-muted-foreground">{user().displayName || user().email}</span>
              </div>}
            </Show>
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
          </>}
        >
          {props.children}
        </ApplicationFrame>
      </Match>
    </Switch>
  );
};

export default AppShell;
