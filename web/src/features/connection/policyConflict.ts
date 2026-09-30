// Conflict messages distinguish a retryable refreshed target from an unavailable policy target.
export type ConflictOutcome =
  | { status: "refreshed"; version: bigint; conflicts: string[] }
  | { status: "stale"; reason: string };

export function conflictMessage(outcome: ConflictOutcome): string {
  if (outcome.status === "refreshed") {
    const rebased = `Another admin saved this policy first (now v${outcome.version}). Your changes were kept on top of theirs`;
    if (outcome.conflicts.length === 0) {
      return `${rebased} — review the warnings and save again.`;
    }
    // Where both of us changed the same field their value stands, so the admin is told exactly what to re-decide instead of discovering it after saving.
    return `${rebased}, except ${outcome.conflicts.join(", ")}: they changed those too, so theirs are shown. Re-apply what you need and save again.`;
  }
  // Both facts, because either alone misleads: naming only the refusal invites a retry that cannot succeed (the token here is still the stale one), and naming only the refresh failure hides why the save was rejected at all.
  return `Another admin saved this policy first, and the new version could not be loaded (${outcome.reason}). Your edits are still here — close and reopen this dialog to apply them on top.`;
}
