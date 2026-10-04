import type { ParentComponent } from "solid-js";
import { Show, onCleanup, onMount } from "solid-js";
import { Button } from "@/shared/ui/button";
import { returnFocusToOpener } from "@/shared/ui/InlinePanel/focusReturn";

/** Reveals a routine workflow in the document, focuses its first field, and returns focus to its opener on close. */
export const InlinePanel: ParentComponent<{ open: boolean; label: string; onClose: () => void }> = props => {
  const Body: ParentComponent = content => {
    let section: HTMLElement | undefined;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : undefined;
    onMount(() => {
      const panel = section;
      if (!panel) return;
      panel.scrollIntoView({ block: "start" });
      const focusFirstField = (): boolean => {
        const field = panel.querySelector<HTMLElement>('input:not([disabled]), textarea:not([disabled]), select:not([disabled])');
        field?.focus({ preventScroll: true });
        return field !== null;
      };
      if (focusFirstField()) return;
      panel.focus({ preventScroll: true });
      // Panels that load their form asynchronously move focus to the first field once it renders, unless the user has moved focus meanwhile.
      const fieldWatcher = new MutationObserver(() => {
        if (document.activeElement === panel && focusFirstField()) fieldWatcher.disconnect();
      });
      fieldWatcher.observe(panel, { childList: true, subtree: true });
      onCleanup(() => fieldWatcher.disconnect());
    });
    onCleanup(() => {
      const active = document.activeElement;
      // Focus is still inside the closing panel, or already fell back to the body as the panel's nodes left the document.
      const panelHeldFocus = active === null || active === document.body || (section?.contains(active) ?? false);
      // Return focus after the closing update settles: openers are commonly disabled while their panel is open, and a disabled control ignores focus().
      queueMicrotask(() => returnFocusToOpener(opener, panelHeldFocus));
    });
    return <section ref={element => { section = element; }} aria-label={props.label} tabindex="-1" class="flex min-w-0 flex-col gap-4 rounded-md border bg-background p-6">
      <div class="flex justify-end"><Button variant="ghost" size="sm" onClick={props.onClose}>Dismiss</Button></div>
      {content.children}
    </section>;
  };
  return <Show when={props.open}><Body>{props.children}</Body></Show>;
};
