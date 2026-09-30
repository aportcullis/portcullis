import type { Component } from "solid-js";
import { For } from "solid-js";

import type { EnvironmentValue } from "@/entities/connection/model";
import { ENVIRONMENTS, parseEnvironment } from "@/entities/connection/model";
import { TextField, TextFieldLabel, TextFieldTextArea } from "@/shared/ui/text-field";

// DescriptorFields is the shared environment + description form body used by the create and edit dialogs (the display name stays with each dialog — its ids and requiredness differ). A raw styled <select> follows the TLS-mode precedent (no vendored Select).
export const DescriptorFields: Component<{
  idPrefix: string;
  environment: EnvironmentValue;
  description: string;
  onEnvironment: (value: EnvironmentValue) => void;
  onDescription: (value: string) => void;
  disabled?: boolean;
}> = (props) => (
  <>
    <div class="flex flex-col gap-1.5">
      <label class="text-sm font-medium leading-none" for={`${props.idPrefix}-environment`}>
        Environment
      </label>
      <select
        id={`${props.idPrefix}-environment`}
        class="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
        disabled={props.disabled}
        value={props.environment}
        onChange={(e) => {
          const env = parseEnvironment(e.currentTarget.value);
          if (env) props.onEnvironment(env);
        }}
      >
        <For each={ENVIRONMENTS}>{(env) => <option value={env.value}>{env.label}</option>}</For>
      </select>
    </div>
    <TextField>
      <TextFieldLabel for={`${props.idPrefix}-description`}>Description</TextFieldLabel>
      <TextFieldTextArea
        id={`${props.idPrefix}-description`}
        disabled={props.disabled}
        maxLength={500}
        placeholder="Optional — what this database is, who owns it, how to treat it."
        value={props.description}
        onInput={(e) => props.onDescription(e.currentTarget.value)}
      />
    </TextField>
  </>
);
