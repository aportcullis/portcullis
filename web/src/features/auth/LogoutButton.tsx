import type { Component } from "solid-js";
import { createSignal } from "solid-js";

import { logout } from "@/entities/session/store";
import { Button } from "@/shared/ui/button";

// Logout revokes the server session; the reactive session store flips to
// anonymous and the route guard redirects — no navigation logic here.
export const LogoutButton: Component = () => {
  const [pending, setPending] = createSignal(false);
  const click = async () => {
    setPending(true);
    try {
      await logout();
    } finally {
      setPending(false);
    }
  };
  return (
    <Button variant="outline" disabled={pending()} onClick={click}>
      Sign out
    </Button>
  );
};
