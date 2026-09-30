import type { Component } from "solid-js";
import { Show } from "solid-js";

import { Navigate, useNavigate } from "@solidjs/router";

import { ConfigErrorCard } from "@/entities/instance/ConfigErrorCard";
import { loginConfig, refetchLoginConfig } from "@/entities/instance/config";
import { BootstrapForm } from "@/features/auth/BootstrapForm";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/shared/ui/card";

// Hide bootstrap while loading and redirect after initialization; the server independently rejects repeated bootstrap.
const BootstrapPage: Component = () => {
  const navigate = useNavigate();
  return (
    // Guard the resource error before reading loginConfig() (which throws when errored) — a GetConfig blip must show a retry, not a broken page.
    <Show when={!loginConfig.error} fallback={<ConfigErrorCard />}>
      <Show when={loginConfig()} keyed>
        {(config) => (
        <Show when={config.needsBootstrap} fallback={<Navigate href="/login" />}>
          <main class="flex min-h-screen items-center justify-center p-4">
            <Card class="w-full max-w-sm">
              <CardHeader>
                <CardTitle>Set up Portcullis</CardTitle>
                <CardDescription>Create the first administrator account</CardDescription>
              </CardHeader>
              <CardContent>
                <BootstrapForm
                  onSuccess={() => {
                    // The install state changed; refresh it before routing or the login page would bounce back to the stale first-run form.
                    void (async () => {
                      await refetchLoginConfig();
                      navigate("/login", { replace: true });
                    })();
                  }}
                />
              </CardContent>
              </Card>
            </main>
          </Show>
        )}
      </Show>
    </Show>
  );
};

export default BootstrapPage;
