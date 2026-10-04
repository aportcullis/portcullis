import { ConnectError } from "@connectrpc/connect";

/** Returns a Connect error's server message without the code prefix, or a generic message for any other failure. */
export function errorMessage(err: unknown): string {
  // Server messages are generic and classified by design and never echo SQL, parameters or credentials (PRD §8.1, ADR-0014), so they are safe to show.
  if (err instanceof ConnectError) {
    return err.rawMessage;
  }
  return "Request failed.";
}
