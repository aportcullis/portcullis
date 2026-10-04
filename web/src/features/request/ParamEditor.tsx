import type { Component } from "solid-js";
import { For, Index, Show } from "solid-js";

import { paramTypes } from "@/entities/request/model";
import type { RequestDraft } from "@/features/request/draft";
import { appendDraftParameter, removeDraftParameter, updateDraftParameter, isParameterValueDisabled, parseParameterType } from "@/features/request/draft";
import { Button } from "@/shared/ui/button";
import { TextField, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

const selectClass =
  "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm";

// ParamEditor edits the typed parameter rows of a request draft (PRD §4.2): name, type (the 8-value vocabulary), and value — the value input goes inert for a null parameter. Named parameters bind server-side (never string substitution); this form only declares them.
export const ParamEditor: Component<{
  draft: RequestDraft;
  onChange: (draft: RequestDraft) => void;
  disabled?: boolean;
}> = (props) => {
  return (
    <div class="flex flex-col gap-3">
      <div class="flex items-center justify-between">
        <span class="text-sm font-medium">Parameters</span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={props.disabled}
          onClick={() => props.onChange(appendDraftParameter(props.draft))}
        >
          Add parameter
        </Button>
      </div>
      <Show
        when={props.draft.params.length > 0}
        fallback={<p class="text-sm text-muted-foreground">No parameters. Use :name in the SQL to bind one.</p>}
      >
        {/* Index keys rows by position: each edit replaces the row object, and reference keying would recreate the focused input. */}
        <Index each={props.draft.params}>
          {(row, parameterIdx) => (
            <div class="flex items-end gap-2">
              <TextField class="flex-1">
                <TextFieldLabel for={`param-name-${parameterIdx}`}>Name</TextFieldLabel>
                <TextFieldInput
                  id={`param-name-${parameterIdx}`}
                  value={row().name}
                  disabled={props.disabled}
                  onInput={(event) => props.onChange(updateDraftParameter(props.draft, parameterIdx, { name: event.currentTarget.value }))}
                />
              </TextField>
              <div class="flex w-32 flex-col gap-1">
                <label class="text-sm font-medium" for={`param-type-${parameterIdx}`}>
                  Type
                </label>
                <select
                  id={`param-type-${parameterIdx}`}
                  class={selectClass}
                  value={row().type}
                  disabled={props.disabled}
                  onChange={(event) => {
                    // The DOM hands back a string; only a value the parser recognizes becomes a parameter type.
                    const parameterType = parseParameterType(event.currentTarget.value);
                    if (parameterType) props.onChange(updateDraftParameter(props.draft, parameterIdx, { type: parameterType }));
                  }}
                >
                  <For each={paramTypes}>{(parameterType) => <option value={parameterType}>{parameterType}</option>}</For>
                </select>
              </div>
              <TextField class="flex-1">
                <TextFieldLabel for={`param-value-${parameterIdx}`}>Value</TextFieldLabel>
                <TextFieldInput
                  id={`param-value-${parameterIdx}`}
                  value={row().value}
                  disabled={props.disabled || isParameterValueDisabled(row().type)}
                  onInput={(event) => props.onChange(updateDraftParameter(props.draft, parameterIdx, { value: event.currentTarget.value }))}
                />
              </TextField>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={props.disabled}
                onClick={() => props.onChange(removeDraftParameter(props.draft, parameterIdx))}
              >
                Remove
              </Button>
            </div>
          )}
        </Index>
      </Show>
    </div>
  );
};
