import type { Component, JSX } from "solid-js";

interface ApplicationFrameProps {
  brand: JSX.Element;
  navigation: JSX.Element;
  account: JSX.Element;
  children?: JSX.Element;
}

/** Arranges application slots without owning session, navigation or permission logic. */
export const ApplicationFrame: Component<ApplicationFrameProps> = (props) => (
  <div class="application-frame">
    <header class="application-header">
      <div class="application-header-content">
        <div class="application-navigation">{props.brand}{props.navigation}</div>
        <div class="application-account">{props.account}</div>
      </div>
    </header>
    <main class="application-content">{props.children}</main>
  </div>
);
