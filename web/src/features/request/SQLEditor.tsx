import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";
import { formatPostgresSQL } from "@/shared/lib/sqlFormatting";
import { Button } from "@/shared/ui/button";
import { TextField, TextFieldLabel, TextFieldTextArea } from "@/shared/ui/text-field";

/** Edits SQL with local format-on-blur, explicit formatting and one-step undo. */
export const SQLEditor: Component<{ id: string; sql: string; onChange: (sql: string) => void; disabled?: boolean }> = props => {
  const [automatic, setAutomatic] = createSignal(true);
  const [previous, setPrevious] = createSignal<string>();
  const [message, setMessage] = createSignal("");
  const formatSQL = () => {
    if (props.disabled) return;
    const result = formatPostgresSQL(props.sql);
    setMessage(result.error);
    if (result.sql === props.sql) return;
    setPrevious(props.sql);
    props.onChange(result.sql);
  };
  const undo = () => {
    const sql = previous();
    if (sql === undefined || props.disabled) return;
    props.onChange(sql);
    setPrevious();
    setMessage("");
    setAutomatic(false);
  };
  return <div class="flex flex-col gap-2">
    <TextField>
      <TextFieldLabel for={props.id}>SQL</TextFieldLabel>
      <TextFieldTextArea id={props.id} class="min-h-80 font-mono" value={props.sql} disabled={props.disabled}
        aria-describedby={`${props.id}-format-status`}
        onInput={event => { setPrevious(); setMessage(""); props.onChange(event.currentTarget.value); }}
        onBlur={event => {
          const next = event.relatedTarget;
          if (automatic() && !(next instanceof Element && next.closest("[data-sql-format-controls]"))) formatSQL();
        }} />
    </TextField>
    <div data-sql-format-controls class="flex flex-wrap items-center gap-3">
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" checked={automatic()} disabled={props.disabled}
        onChange={event => setAutomatic(event.currentTarget.checked)} />Auto-format SQL</label>
      <Button type="button" variant="outline" size="sm" disabled={props.disabled} onClick={formatSQL}>Format SQL</Button>
      <Show when={previous() !== undefined}><Button type="button" variant="ghost" size="sm" disabled={props.disabled} onClick={undo}>Undo formatting</Button></Show>
    </div>
    <p id={`${props.id}-format-status`} role="status" class="text-xs text-muted-foreground">{message() || "Auto-format runs when you leave SQL. Review your statement before submitting."}</p>
  </div>;
};
