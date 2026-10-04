import type { Component } from "solid-js";
import { Show, untrack } from "solid-js";

import { Navigate, useNavigate, useSearchParams } from "@solidjs/router";

import { ConfigErrorCard } from "@/entities/instance/ConfigErrorCard";
import { loginConfig } from "@/entities/instance/config";
import { session } from "@/entities/session/store";
import { GoogleLoginButton } from "@/features/auth/GoogleLoginButton";
import { LoginForm } from "@/features/auth/LoginForm";
import { returnPathParameter, sanitizeReturnPath } from "@/shared/lib/returnPath";
import { BrandLogo } from "@/shared/ui/BrandLogo";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/shared/ui/card";

const LoginPage: Component = () => {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  // A deep link that required sign-in continues where it started; anything but a same-origin path lands on the start page.
  // Read once at mount: after navigating away, the session update re-renders this page against the new URL, whose query no longer carries the return path.
  const initialReturnPath = untrack(() => sanitizeReturnPath(params[returnPathParameter]));
  const returnPath = () => initialReturnPath;

  return (
    // Check the resource's error BEFORE reading loginConfig() — reading an errored resource throws, which would break the page with no recovery.
    <Show when={!loginConfig.error} fallback={<ConfigErrorCard />}>
      <Show when={session().status !== "authenticated"} fallback={<Navigate href={returnPath()} />}>
        <Show when={!loginConfig()?.needsBootstrap} fallback={<Navigate href="/bootstrap" />}>
        <main class="flex min-h-screen items-center justify-center p-4">
          <Card class="w-full max-w-sm">
            <CardHeader>
              <CardTitle><BrandLogo class="h-12" /></CardTitle>
              <CardDescription>Sign in to your account</CardDescription>
            </CardHeader>
            <CardContent class="flex flex-col gap-4">
              <Show when={params.error === "oidc"}>
                <Alert variant="destructive">
                  <AlertDescription>Google sign-in failed. Please retry.</AlertDescription>
                </Alert>
              </Show>
              <LoginForm onSuccess={() => navigate(returnPath(), { replace: true })} />
                <Show when={loginConfig()?.googleEnabled}>
                  <GoogleLoginButton />
                </Show>
              </CardContent>
            </Card>
          </main>
        </Show>
      </Show>
    </Show>
  );
};

export default LoginPage;
