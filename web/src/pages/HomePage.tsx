import type { Component } from "solid-js";
import { Match, Switch } from "solid-js";

import { Navigate } from "@solidjs/router";

import { UnreachableCard } from "@/entities/session/UnreachableCard";
import { session } from "@/entities/session/store";
import { LogoutButton } from "@/features/auth/LogoutButton";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/shared/ui/card";

const HomePage: Component = () => (
  <Switch>
    <Match when={session().status === "anonymous"}>
      <Navigate href="/login" />
    </Match>
    <Match when={session().status === "unreachable"}>
      <UnreachableCard />
    </Match>
    <Match when={session().status === "authenticated"}>
      <main class="flex min-h-screen items-center justify-center p-4">
        <Card class="w-full max-w-md">
          <CardHeader>
            <CardTitle>Portcullis</CardTitle>
            <CardDescription>
              {(() => {
                const s = session();
                return s.status === "authenticated" ? `Signed in as ${s.user.email}` : "";
              })()}
            </CardDescription>
          </CardHeader>
          <CardContent class="flex flex-col gap-4">
            <a class="text-sm underline-offset-4 hover:underline" href="/connections">
              Connections →
            </a>
            <LogoutButton />
          </CardContent>
        </Card>
      </main>
    </Match>
  </Switch>
);

export default HomePage;
