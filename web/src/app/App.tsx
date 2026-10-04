import type { Component } from "solid-js";
import { ErrorBoundary, createEffect, lazy, onMount } from "solid-js";

import { Navigate, Route, Router } from "@solidjs/router";

import "@/app/index.css";
import { ApplicationErrorCard } from "@/app/ApplicationErrorCard";
import AppShell from "@/app/AppShell";
import { landingFor } from "@/app/navigation";
import { resetConnections } from "@/entities/connection/store";
import { resetAccessRequests, setMayListAccessRequests } from "@/entities/request/store";
import { hasPermission, session } from "@/entities/session/store";
import BootstrapPage from "@/pages/BootstrapPage";
import LoginPage from "@/pages/LoginPage";
import NotFoundPage from "@/pages/NotFoundPage";
import { load } from "@/entities/session/store";

// Sign-in and first-run stay in the entry chunk; authenticated workflows load on first visit so the login page does not download the SQL editor, result grid and connection forms.
const ConnectionsPage = lazy(() => import("@/pages/ConnectionsPage"));
const RequestsPage = lazy(() => import("@/pages/RequestsPage"));
const NewRequestPage = lazy(() => import("@/pages/NewRequestPage"));
const RequestDetailsPage = lazy(() => import("@/pages/RequestDetailsPage"));
const RequestResultPage = lazy(() => import("@/pages/RequestResultPage"));

// Composition root: global CSS, the router, the one-time session resolve, and the cross-entity lifecycle wiring (a leaf entity must not import another, so the "purge a principal's cached data when the principal changes" rule lives here, at the composition root that legitimately sees both entities).
const App: Component = () => {
  onMount(() => void load());

  // Clear the connection cache whenever the authenticated principal changes or goes away — a later, less-privileged login in the same tab must never see the previous user's descriptors (OWASP: purge client-side data on session end). resetConnections also fences any in-flight load from the old user.
  let lastUserID = "";
  createEffect(() => {
    const principal = session();
    const userID = principal.status === "authenticated" ? principal.user.id : "";
    if (userID !== lastUserID) {
      lastUserID = userID;
      resetConnections();
      resetAccessRequests();
    }
    // The composition root supplies list capability so mutation refreshes do not issue requests.list for create-only roles.
    setMayListAccessRequests(hasPermission("requests.list"));
  });

  // Authenticated routes share AppShell; the root selects the first section available to the caller’s capability union.
  const landing = () => landingFor(hasPermission);

  // The boundary turns an unexpected render failure into a recoverable card instead of a blank document; unknown authenticated addresses get an explicit not-found page.
  return (
    <Router root={(props) => (
      <ErrorBoundary fallback={(error: unknown, reset) => {
        console.error(error);
        return <ApplicationErrorCard onRetry={reset} />;
      }}>
        {props.children}
      </ErrorBoundary>
    )}>
      <Route path="/login" component={LoginPage} />
      <Route path="/bootstrap" component={BootstrapPage} />
      <Route component={AppShell}>
        <Route path="/" component={() => <Navigate href={landing()} />} />
        <Route path="/connections" component={ConnectionsPage} />
        <Route path="/requests" component={RequestsPage} />
        <Route path="/requests/new" component={NewRequestPage} />
        <Route path="/requests/:id" component={RequestDetailsPage} />
        <Route path="/requests/:id/result" component={RequestResultPage} />
        <Route path="*404" component={NotFoundPage} />
      </Route>
    </Router>
  );
};

export default App;
