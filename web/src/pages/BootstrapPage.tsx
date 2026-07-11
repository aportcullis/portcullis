import type { Component } from "solid-js";

import { useNavigate } from "@solidjs/router";

import { refetchLoginConfig } from "@/entities/instance/config";
import { BootstrapForm } from "@/features/auth/BootstrapForm";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/shared/ui/card";

const BootstrapPage: Component = () => {
  const navigate = useNavigate();
  return (
    <main class="flex min-h-screen items-center justify-center p-4">
      <Card class="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Set up Portcullis</CardTitle>
          <CardDescription>Create the first administrator account</CardDescription>
        </CardHeader>
        <CardContent>
          <BootstrapForm
            onSuccess={() => {
              // The install state changed; refresh it before routing or the
              // login page would bounce back to the stale first-run form.
              void (async () => {
                await refetchLoginConfig();
                navigate("/login", { replace: true });
              })();
            }}
          />
        </CardContent>
        <CardFooter class="text-sm text-muted-foreground">
          <a class="underline underline-offset-4" href="/login">
            Back to sign in
          </a>
        </CardFooter>
      </Card>
    </main>
  );
};

export default BootstrapPage;
