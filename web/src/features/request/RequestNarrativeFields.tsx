import type { Component } from "solid-js";
import { Show } from "solid-js";

import { loginConfig } from "@/entities/instance/config";
import type { RequestDraft } from "@/features/request/draft";

/** Edits a request's title and explanatory body within the page. */
export const RequestNarrativeFields: Component<{
  id: string;
  draft: RequestDraft;
  disabled: boolean;
  onChange: (draft: RequestDraft) => void;
}> = (props) => (
  <>
    <div class="flex flex-col gap-1">
      <label class="text-sm font-medium" for={`${props.id}-title`}>Title</label>
      <input
        id={`${props.id}-title`}
        required
        class="h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
        value={props.draft.title}
        disabled={props.disabled}
        aria-describedby={`${props.id}-title-help`}
        onInput={e => props.onChange({ ...props.draft, title: e.currentTarget.value })}
        placeholder="Describe the work you want reviewed"
      />
      <p id={`${props.id}-title-help`} class="text-xs text-muted-foreground">Visible in the request list. Do not include secrets.<Show when={loginConfig()?.maxRequestTitleChars}>{limit => <> {[...props.draft.title.trim()].length} / {limit()} characters.</>}</Show></p>
    </div>
    <div class="flex flex-col gap-1">
      <label class="text-sm font-medium" for={`${props.id}-body`}>Body (optional)</label>
      <textarea
        id={`${props.id}-body`}
        rows={5}
        class="w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm"
        value={props.draft.body}
        disabled={props.disabled}
        aria-describedby={`${props.id}-body-help`}
        onInput={e => props.onChange({ ...props.draft, body: e.currentTarget.value })}
        placeholder="Explain the purpose, expected impact and anything reviewers should check."
      />
      <p id={`${props.id}-body-help`} class="text-xs text-muted-foreground">Plain text; only visible in authorized request details. Do not include secrets.<Show when={loginConfig()?.maxRequestBodyChars}>{limit => <> {[...props.draft.body].length} / {limit()} characters.</>}</Show></p>
    </div>
  </>
);
