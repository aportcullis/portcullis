import type { Component } from "solid-js";
import { onMount } from "solid-js";

import { Route, Router } from "@solidjs/router";

import "@/app/index.css";
import { load } from "@/entities/session/store";
import BootstrapPage from "@/pages/BootstrapPage";
import HomePage from "@/pages/HomePage";
import LoginPage from "@/pages/LoginPage";

// Composition root: global CSS, the router, and the one-time session resolve.
const App: Component = () => {
  onMount(() => void load());
  return (
    <Router>
      <Route path="/" component={HomePage} />
      <Route path="/login" component={LoginPage} />
      <Route path="/bootstrap" component={BootstrapPage} />
    </Router>
  );
};

export default App;
