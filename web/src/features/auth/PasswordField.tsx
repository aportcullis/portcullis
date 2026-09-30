import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import { TextField, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

// A password input with a show/hide toggle. Revealing is a deliberate, reversible act scoped to this field only — the value never leaves the controlled signal, and the toggle is a plain button so it cannot submit the surrounding form.
export const PasswordField: Component<{
  id: string;
  label: string;
  autocomplete: "current-password" | "new-password";
  value: string;
  onInput: (value: string) => void;
  minlength?: number;
}> = (props) => {
  const [visible, setVisible] = createSignal(false);

  return (
    <TextField>
      <TextFieldLabel for={props.id}>{props.label}</TextFieldLabel>
      <div class="relative">
        <TextFieldInput
          id={props.id}
          type={visible() ? "text" : "password"}
          autocomplete={props.autocomplete}
          required
          minlength={props.minlength}
          class="pr-10"
          value={props.value}
          onInput={(e) => props.onInput(e.currentTarget.value)}
        />
        <button
          type="button"
          class="absolute inset-y-0 right-0 flex w-10 items-center justify-center text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label={visible() ? "Hide password" : "Show password"}
          aria-pressed={visible()}
          onClick={() => setVisible((v) => !v)}
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            class="size-4"
          >
            <Show
              when={visible()}
              fallback={
                <>
                  <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" />
                  <circle cx="12" cy="12" r="3" />
                </>
              }
            >
              <path d="M9.88 9.88a3 3 0 1 0 4.24 4.24" />
              <path d="M10.73 5.08A10.4 10.4 0 0 1 12 5c6.5 0 10 7 10 7a13.2 13.2 0 0 1-1.67 2.68" />
              <path d="M6.61 6.61A13.5 13.5 0 0 0 2 12s3.5 7 10 7a9.7 9.7 0 0 0 5.39-1.61" />
              <line x1="2" x2="22" y1="2" y2="22" />
            </Show>
          </svg>
        </button>
      </div>
    </TextField>
  );
};
