import type { Component } from "solid-js";
import { createEffect, onMount } from "solid-js";

import { Navigate, Route, Router } from "@solidjs/router";

import "@/app/index.css";
import AppShell from "@/app/AppShell";
import { resetConnections } from "@/entities/connection/store";
import { session } from "@/entities/session/store";
import BootstrapPage from "@/pages/BootstrapPage";
import ConnectionsPage from "@/pages/ConnectionsPage";
import LoginPage from "@/pages/LoginPage";
import { load } from "@/entities/session/store";

// Composition root: global CSS, the router, the one-time session resolve, and
// the cross-entity lifecycle wiring (a leaf entity must not import another, so
// the "purge a principal's cached data when the principal changes" rule lives
// here, at the composition root that legitimately sees both entities).
const App: Component = () => {
  onMount(() => void load());

  // Clear the connection cache whenever the authenticated principal changes or
  // goes away — a later, less-privileged login in the same tab must never see
  // the previous user's descriptors (OWASP: purge client-side data on session
  // end). resetConnections also fences any in-flight load from the old user.
  let lastUserID = "";
  createEffect(() => {
    const s = session();
    const userID = s.status === "authenticated" ? s.user.id : "";
    if (userID !== lastUserID) {
      lastUserID = userID;
      resetConnections();
    }
  });

  // Authenticated pages live under AppShell (one route guard + the header);
  // login/bootstrap render bare. "/" lands straight on the connections table —
  // the Google OIDC callback redirects to "/" (ADR-0007) and arrives there too.
  return (
    <Router>
      <Route path="/login" component={LoginPage} />
      <Route path="/bootstrap" component={BootstrapPage} />
      <Route component={AppShell}>
        <Route path="/" component={() => <Navigate href="/connections" />} />
        <Route path="/connections" component={ConnectionsPage} />
      </Route>
    </Router>
  );
};

export default App;
