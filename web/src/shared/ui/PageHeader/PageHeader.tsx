import type { Component, JSX } from "solid-js";
import { Show } from "solid-js";

/** Presents a consistent page title, purpose and optional primary actions. */
export const PageHeader: Component<{ title: string; description: string; eyebrow?: string; actions?: JSX.Element }> = (props) => (
  <header class="page-heading">
    <div>
      <Show when={props.eyebrow}><div class="page-eyebrow">{props.eyebrow}</div></Show>
      <h1>{props.title}</h1>
      <p>{props.description}</p>
    </div>
    {props.actions}
  </header>
);
