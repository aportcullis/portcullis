import type { Component } from "solid-js";
import { createEffect, onMount } from "solid-js";

import { Route, Router } from "@solidjs/router";

import "@/app/index.css";
import { resetConnections } from "@/entities/connection/store";
import { session } from "@/entities/session/store";
import BootstrapPage from "@/pages/BootstrapPage";
import ConnectionsPage from "@/pages/ConnectionsPage";
import HomePage from "@/pages/HomePage";
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

  return (
    <Router>
      <Route path="/" component={HomePage} />
      <Route path="/login" component={LoginPage} />
      <Route path="/bootstrap" component={BootstrapPage} />
      <Route path="/connections" component={ConnectionsPage} />
    </Router>
  );
};

export default App;
