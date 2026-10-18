import type { JSX } from "solid-js";
import { Show, createSignal, createUniqueId, onCleanup } from "solid-js";

export { createManagementAction } from "@/features/administration/action";

import { createDialogSession } from "@/shared/lib/dialogSession";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/shared/ui/dialog";
import { TextField, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

/** Accessible field shared only within management interactions. */
export function ManagementField(props: { label: string; value: string; onInput: (value: string) => void; type?: "text" | "email"; required?: boolean; disabled?: boolean }) {
  return <TextField value={props.value} onChange={props.onInput}><TextFieldLabel>{props.label}</TextFieldLabel><TextFieldInput type={props.type ?? "text"} required={props.required} disabled={props.disabled} /></TextField>;
}

/** A labeled native select retains keyboard and narrow-screen behavior. */
export function ManagementSelect(props: { label: string; value: string; onChange: (value: string) => void; children: JSX.Element; disabled?: boolean }) {
  const identifier = createUniqueId();
  return <div class="flex flex-col gap-1 text-sm"><label for={identifier}>{props.label}</label><select id={identifier} class="h-10 max-w-full rounded-md border border-input bg-background px-2" value={props.value} disabled={props.disabled} onChange={event => props.onChange(event.currentTarget.value)}>{props.children}</select></div>;
}

/** Renders generic feedback without interpreting storage or CEL source messages. */
export function ManagementFeedback(props: { error: string; message?: string }) {
  return <><Show when={props.error}><p role="alert" class="rounded-md border border-destructive p-3 text-sm text-destructive">{props.error}</p></Show><Show when={props.message}><p role="status" class="text-sm">{props.message}</p></Show></>;
}

/** Requires an explicit modal confirmation for account removal and grant changes. */
export function ConfirmManagementAction(props: { label: string; description: string; disabled?: boolean; onConfirm: () => Promise<unknown> }) {
  const [open, setOpen] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const lifetime = createDialogSession();
  onCleanup(lifetime.discardSession);
  /** Closing releases the dialog without letting a late completion close its next opening. */
  const changeOpen = (next: boolean) => { lifetime.discardSession(); setBusy(false); setOpen(next); };
  return <Dialog open={open()} onOpenChange={changeOpen}><DialogTrigger as={Button} variant="outline" size="sm" disabled={props.disabled}>{props.label}</DialogTrigger><DialogContent><DialogHeader><DialogTitle>{props.label}?</DialogTitle><DialogDescription>{props.description}</DialogDescription></DialogHeader><DialogFooter><Button variant="outline" onClick={() => changeOpen(false)}>Cancel</Button><Button variant="destructive" disabled={busy() || props.disabled} onClick={() => { setBusy(true); void lifetime.runInSession(props.onConfirm).then(outcome => { if (outcome.status !== "superseded") changeOpen(false); }); }}>Confirm</Button></DialogFooter></DialogContent></Dialog>;
}

/** Makes a narrow committed identity receipt usable without requiring a separate read. */
export function ManagementIDReceipt(props: { label: string; id: string; version?: bigint }) {
  return <div class="flex flex-col gap-1"><label class="flex flex-col gap-1 text-sm">{props.label}<input class="w-full rounded border border-input bg-background p-2 font-mono text-sm" readOnly value={props.id} /></label><Show when={props.version !== undefined}><p class="text-sm">Committed version {props.version?.toString()}</p></Show></div>;
}
