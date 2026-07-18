/* @refresh reload */
import { render } from "solid-js/web";

import App from "@/app/App";

const root = document.getElementById("root");
if (!root) {
  throw new Error("application root is missing");
}
render(() => <App />, root);
