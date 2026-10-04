import { createSignal } from "solid-js";

import type { ConfigDraft, TestResult } from "@/entities/connection/model";
import { emptyDraft } from "@/entities/connection/model";
import { testDraft } from "@/entities/connection/store";
import { errorMessage } from "@/shared/api/errors";

// createDraftController fences pre-save tests by draft revision so input changes invalidate old results. The server tests again on save.
export function createDraftController() {
  const [draft, setDraft] = createSignal<ConfigDraft>(emptyDraft());
  const [testResult, setTestResult] = createSignal<TestResult | null>(null);
  const [testError, setTestError] = createSignal("");
  const [testing, setTesting] = createSignal(false);
  let revision = 0;
  let testRun = 0;

  const patch = (changes: Partial<ConfigDraft>) => {
    revision++;
    setDraft({ ...draft(), ...changes });
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
