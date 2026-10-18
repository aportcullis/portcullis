import { For } from "solid-js";

import { conditionResourceTypes, type ConditionChoices } from "@/entities/administration/condition";
import { ManagementField, ManagementSelect } from "@/features/administration/controls";

/** Generates constrained choices without exposing an expression editor. */
export function ConditionFields(props: { value: ConditionChoices; onChange: (value: ConditionChoices) => void }) {
  /** Replaces one controlled choice without mutating the original source. */
  const change = (key: keyof ConditionChoices, value: string) => props.onChange({ ...props.value, [key]: value });
  return <fieldset class="flex flex-col gap-3 rounded border p-3"><legend>Condition restrictions</legend>
    <ManagementField label="Condition title" value={props.value.title} onInput={value => change("title", value)} required />
    <ManagementField label="Starts at (ISO timestamp)" value={props.value.startsAt} onInput={value => change("startsAt", value)} />
    <ManagementField label="Expires at (ISO timestamp)" value={props.value.expiresAt} onInput={value => change("expiresAt", value)} />
    <p class="text-sm text-muted-foreground">Use an explicit time zone, for example 2030-01-01T12:00:00Z. Start is inclusive; expiry is exclusive. Selected restrictions must all match.</p>
    <ManagementSelect label="Resource type" value={props.value.resourceType} onChange={value => change("resourceType", value)}><option value="">Any type</option><For each={conditionResourceTypes}>{kind => <option value={kind}>{kind}</option>}</For></ManagementSelect>
    <ManagementField label="Resource ID restriction" value={props.value.resourceID} onInput={value => change("resourceID", value)} />
  </fieldset>;
}
