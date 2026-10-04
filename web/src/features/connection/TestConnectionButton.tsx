import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import type { TestResult } from "@/entities/connection/store";
import { testSaved } from "@/entities/connection/store";
import { errorMessage } from "@/shared/api/errors";
import { Button } from "@/shared/ui/button";

// TestConnectionButton re-tests a SAVED connection: the server decrypts the stored credential and dials (the credential never travels to the browser).
export const TestConnectionButton: Component<{ id: string }> = (props) => {
  const [result, setResult] = createSignal<TestResult | null>(null);
  const [pending, setPending] = createSignal(false);

  const run = async () => {
    setResult(null);
    setPending(true);
    try {
      setResult(await testSaved(props.id));
    } catch (err) {
      setResult({ ok: false, message: errorMessage(err) });
    } finally {
      setPending(false);
    }
  };

  return (
    <span class="inline-flex items-center gap-2">
      <Button size="sm" variant="outline" disabled={pending()} onClick={() => void run()}>
        {pending() ? "Testing…" : "Test"}
      </Button>
      <Show when={result()}>
        {(testResult) => (
          <span class={testResult().ok ? "text-sm text-muted-foreground" : "text-sm text-destructive"}>
            {testResult().ok ? "OK" : testResult().message}
          </span>
        )}
      </Show>
    </span>
  );
};
