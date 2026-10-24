import type { Component } from "solid-js";
import { For, Match, Show, Suspense, Switch, createEffect, onCleanup } from "solid-js";

import type { RouteSectionProps } from "@solidjs/router";
import { Navigate, useLocation } from "@solidjs/router";

import { signInRedirectFor, visibleSections } from "@/app/navigation";
import { createPendingRequests } from "@/app/pendingRequests";
import { configRetryDelayMs, loginConfig, refetchLoginConfig } from "@/entities/instance/config";
import { UnreachableCard } from "@/entities/session/UnreachableCard";
import { hasPermission, session } from "@/entities/session/store";
import { AccountMenu } from "@/features/auth/AccountMenu";
import { ApplicationFrame } from "@/shared/ui/ApplicationFrame";
import { BrandLogo } from "@/shared/ui/BrandLogo";
import { LoadingSkeleton } from "@/shared/ui/LoadingSkeleton";
import { Badge } from "@/shared/ui/badge";

// AppShell owns the session guard and capability-based navigation; the server still authorizes every RPC.
const AppShell: Component<RouteSectionProps> = (props) => {
  const pendingCount = createPendingRequests();
  const location = useLocation();
  // Authenticated pages read the instance limits optionally; a failed config read is retried in the background so the limits return without a reload.
  createEffect(() => {
    if (loginConfig.error === undefined) return;
    const timer = setTimeout(() => void refetchLoginConfig(), configRetryDelayMs);
    onCleanup(() => clearTimeout(timer));
  });
  const anonymousCause = () => {
    const current = session();
    return current.status === "anonymous" ? current.cause : undefined;
  };
  return (
    <Switch>
      <Match when={anonymousCause()}>
        {(cause) => <Navigate href={signInRedirectFor(cause(), location)} />}
      </Match>
      <Match when={session().status === "loading"}>
        <main class="flex min-h-screen items-center justify-center p-4">
          <div class="w-full max-w-sm"><LoadingSkeleton label="Loading your session…" /></div>
        </main>
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
          account={<AccountMenu routeKey={`${location.pathname}${location.search}${location.hash}`} />}
        >
          {/* Authenticated pages are lazy chunks; the frame stays while a page's code loads. */}
          <Suspense fallback={<LoadingSkeleton label="Loading page…" />}>{props.children}</Suspense>
        </ApplicationFrame>
      </Match>
    </Switch>
  );
};

export default AppShell;
