import { ApplicationFrame } from "@/shared/ui/ApplicationFrame";

/** Demonstrates the public ApplicationFrame API without application services. */
export function Example() {
  return (<ApplicationFrame brand={<span>Example</span>} navigation={<nav aria-label="Main">Home</nav>} account={<span>Account</span>}>Content</ApplicationFrame>);
}
