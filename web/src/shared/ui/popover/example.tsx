import { createSignal } from "solid-js";

import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";

/** Demonstrates an inline nonmodal disclosure without account state or requests. */
export function Example() {
  const [open, setOpen] = createSignal(false);
  return (
    <Popover open={open()} onOpenChange={setOpen} modal={false} gutter={8}>
      <PopoverTrigger aria-haspopup={false} class="rounded-md border px-3 py-2">
        Example details
      </PopoverTrigger>
      <PopoverContent role="region" aria-label="Example details">
        <p class="mb-3 text-sm">Ordinary document controls remain available.</p>
        <Button variant="outline" onClick={() => setOpen(false)}>Close details</Button>
      </PopoverContent>
    </Popover>
  );
}
