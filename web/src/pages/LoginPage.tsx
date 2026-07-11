import type { Component } from "solid-js";
import { Show } from "solid-js";

import { Navigate, useNavigate, useSearchParams } from "@solidjs/router";

import { loginConfig } from "@/entities/instance/config";
import { session } from "@/entities/session/store";
import { GoogleLoginButton } from "@/features/auth/GoogleLoginButton";
import { LoginForm } from "@/features/auth/LoginForm";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/shared/ui/card";

const LoginPage: Component = () => {
  const navigate = useNavigate();
  const [params] = useSearchParams();

  return (
    <Show when={session().status !== "authenticated"} fallback={<Navigate href="/" />}>
      <Show when={!loginConfig()?.needsBootstrap} fallback={<Navigate href="/bootstrap" />}>
        <main class="flex min-h-screen items-center justify-center p-4">
          <Card class="w-full max-w-sm">
            <CardHeader>
              <CardTitle>Portcullis</CardTitle>
              <CardDescription>Sign in to your account</CardDescription>
            </CardHeader>
            <CardContent class="flex flex-col gap-4">
              <Show when={params.error === "oidc"}>
                <Alert variant="destructive">
                  <AlertDescription>Google sign-in failed. Please retry.</AlertDescription>
                </Alert>
              </Show>
              <LoginForm onSuccess={() => navigate("/", { replace: true })} />
              <Show when={loginConfig()?.googleEnabled}>
                <GoogleLoginButton />
              </Show>
            </CardContent>
            <CardFooter class="text-sm text-muted-foreground">
              <a class="underline underline-offset-4" href="/bootstrap">
                First run? Create the admin account
              </a>
            </CardFooter>
          </Card>
        </main>
      </Show>
    </Show>
  );
};

export default LoginPage;
