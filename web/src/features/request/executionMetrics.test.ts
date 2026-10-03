import { describe, expect, it } from "vitest";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { formatExecutionDuration } from "@/features/request/executionMetrics";

const recorded = (durationMs: bigint) => ({ state: AccessRequestState.SUCCEEDED, durationMs });

describe("recorded execution duration", () => {
  it("shows completed subsecond measurements and millisecond truncation honestly", () => {
    expect(formatExecutionDuration(recorded(47n))).toBe("47 ms");
    expect(formatExecutionDuration(recorded(0n))).toBe("<1 ms");
    expect(formatExecutionDuration(recorded(999n))).toBe("999 ms");
  });
  it("preserves all recorded milliseconds above one second, including large int64 values", () => {
    expect(formatExecutionDuration(recorded(1000n))).toBe("1.000 s");
    expect(formatExecutionDuration(recorded(1250n))).toBe("1.250 s");
    expect(formatExecutionDuration(recorded(9007199254740993n))).toBe("9007199254740.993 s");
  });
  it("does not turn a running or missing uncertain interval into completed timing", () => {
    expect(formatExecutionDuration({ state: AccessRequestState.EXECUTING, durationMs: 0n })).toBe("Running");
    expect(formatExecutionDuration({ state: AccessRequestState.OUTCOME_UNKNOWN, durationMs: 0n })).toBe("Not available");
    expect(formatExecutionDuration({ state: AccessRequestState.OUTCOME_UNKNOWN, durationMs: 250n })).toBe("250 ms");
    expect(formatExecutionDuration({ state: AccessRequestState.FAILED, durationMs: 250n })).toBe("250 ms");
  });
});
