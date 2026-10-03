import { createSignal } from "solid-js";
import { InlinePanel } from "@/shared/ui/InlinePanel";
import { Button } from "@/shared/ui/button";

/** Demonstrates the public InlinePanel API without application services. */
export function Example() {
  const [open, setOpen] = createSignal(false);
  return (<><Button type="button" onClick={() => setOpen(true)}>Show details</Button><InlinePanel open={open()} label="Item details" onClose={() => setOpen(false)}>Details</InlinePanel></>);
}
