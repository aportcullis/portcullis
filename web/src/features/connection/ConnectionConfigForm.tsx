import type { Component } from "solid-js";
import { Show } from "solid-js";

import { ConfigFields } from "@/features/connection/ConfigFields";
import type { DraftController } from "@/features/connection/draft";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";

// ConnectionConfigForm groups the config fields with their pre-save test — the "Test connection" button and its result live next to the fields they test, driven by a shared draft controller whose revision guard prevents a stale test result from reappearing after an edit (ADR-0014). Shared by the create and edit dialogs.
export const ConnectionConfigForm: Component<{
  controller: DraftController;
  disabled?: boolean;
}> = (props) => (
  <>
    <ConfigFields draft={props.controller.draft} onPatch={props.controller.patch} />
    <Show when={props.controller.testResult()}>
      {(result) => (
        <Alert variant={result().ok ? undefined : "destructive"}>
          <AlertDescription>
            {result().ok
              ? "Connection test succeeded."
              : `Connection test failed: ${result().message}.`}
          </AlertDescription>
        </Alert>
      )}
    </Show>
    <Show when={props.controller.testError() !== ""}>
      <Alert variant="destructive">
        <AlertDescription>{props.controller.testError()}</AlertDescription>
      </Alert>
    </Show>
    <div class="flex justify-end">
      <Button
        type="button"
        variant="outline"
        disabled={props.controller.testing() || props.disabled}
        onClick={() => void props.controller.runTest()}
      >
        {props.controller.testing() ? "Testing…" : "Test connection"}
      </Button>
    </div>
  </>
);
