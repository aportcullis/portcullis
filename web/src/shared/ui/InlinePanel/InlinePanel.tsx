import type { ParentComponent } from "solid-js";
import { Show, onMount } from "solid-js";
import { Button } from "@/shared/ui/button";

/** Reveals a routine workflow in the document and focuses its first field. */
export const InlinePanel: ParentComponent<{ open: boolean; label: string; onClose: () => void }> = props => {
  const Body: ParentComponent = content => {
    let section!: HTMLElement;
    onMount(() => {
      section.scrollIntoView({ block: "start" });
      const input = section.querySelector<HTMLElement>('input:not([disabled]), textarea:not([disabled]), select:not([disabled])');
      (input ?? section).focus({ preventScroll: true });
    });
    return <section ref={element => { section = element; }} aria-label={props.label} tabindex="-1" class="flex min-w-0 flex-col gap-4 rounded-md border bg-background p-6">
      <div class="flex justify-end"><Button variant="ghost" size="sm" onClick={props.onClose}>Dismiss</Button></div>
      {content.children}
    </section>;
  };
  return <Show when={props.open}><Body>{props.children}</Body></Show>;
};
