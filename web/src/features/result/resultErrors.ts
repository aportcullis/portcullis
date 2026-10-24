import { Code, ConnectError } from "@connectrpc/connect";

// The server answers FailedPrecondition for an expired or evicted snapshot and NotFound for a missing execution record.
const snapshotGoneCodes: readonly Code[] = [Code.FailedPrecondition, Code.NotFound];

/** Reports whether a result read or export failed because the cached snapshot no longer exists. */
function isSnapshotGone(err: unknown): boolean {
  return err instanceof ConnectError && snapshotGoneCodes.includes(err.code);
}

/** Explains a failed result read or export, adding the expiry hint only when the snapshot itself is gone. */
export function describeResultError(err: unknown, formatMessage: (err: unknown) => string): string {
  const message = formatMessage(err);
  return isSnapshotGone(err) ? `${message} Results may have expired or been evicted.` : message;
}
