/** The outcome of reading the "Max result (MiB)" input: a byte limit, or the message to show beside the field. */
export type MaxResultParse = { status: "ok"; bytes: bigint } | { status: "invalid"; message: string };

const bytesPerMebibyte = 1048576n;

/** Converts the raw "Max result (MiB)" input into bytes, refusing anything but a positive whole number. */
export function parseMaxResultMiB(raw: string): MaxResultParse {
  const trimmed = raw.trim();
  if (trimmed === "") return { status: "invalid", message: "Enter the max result size in MiB." };
  const mebibytes = /^\d+$/.test(trimmed) ? Number(trimmed) : Number.NaN;
  // The upper bound is the policy validator's; this only refuses what is not a whole number it could check.
  if (!Number.isSafeInteger(mebibytes) || mebibytes < 1) {
    return { status: "invalid", message: "Max result must be a positive whole number of MiB." };
  }
  return { status: "ok", bytes: BigInt(mebibytes) * bytesPerMebibyte };
}
