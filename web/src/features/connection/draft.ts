import { createSignal } from "solid-js";

import type { ConfigDraft, TestResult } from "@/entities/connection/model";
import { emptyDraft } from "@/entities/connection/model";
import { errorMessage, testDraft } from "@/entities/connection/store";

// createDraftController owns a connection-config draft and its pre-save test,
// shared by the create and edit dialogs. A monotonic revision fences the test
// against edits: the inputs stay enabled while a test runs, so a user can
// change a value before the test resolves — a stale success from the OLD
// config must not then reappear against the NEW one. patch()/reset() bump the
// revision; runTest() captures it at the start and only shows a result if the
// draft has not changed since (the server re-tests on save regardless, so this
// is a UX guard, not a security one — ADR-0014).
export function createDraftController() {
  const [draft, setDraft] = createSignal<ConfigDraft>(emptyDraft());
  const [testResult, setTestResult] = createSignal<TestResult | null>(null);
  const [testError, setTestError] = createSignal("");
  const [testing, setTesting] = createSignal(false);
  let revision = 0;
  let testRun = 0;

  const patch = (p: Partial<ConfigDraft>) => {
    revision++;
    setDraft({ ...draft(), ...p });
    setTestResult(null); // a stale result must not vouch for edited coordinates
    setTestError("");
  };

  const reset = (next: ConfigDraft = emptyDraft()) => {
    revision++;
    testRun++;
    setDraft(next);
    setTestResult(null);
    setTestError("");
    setTesting(false);
  };

  const runTest = async () => {
    const rev = revision;
    const run = ++testRun;
    setTesting(true);
    setTestError("");
    try {
      const result = await testDraft(draft());
      if (run === testRun && rev === revision) setTestResult(result);
    } catch (err) {
      if (run === testRun && rev === revision) setTestError(errorMessage(err));
    } finally {
      if (run === testRun) setTesting(false);
    }
  };

  return { draft, patch, reset, runTest, testResult, testError, testing };
}

export type DraftController = ReturnType<typeof createDraftController>;
