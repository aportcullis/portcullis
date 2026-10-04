import { Code, ConnectError } from "@connectrpc/connect";

/** Maps a Bootstrap rejection to user-facing guidance without echoing input back. */
export function describeBootstrapError(error: unknown): string {
  // Bootstrap is refused once any user exists (install-level state, so naming it is not an account oracle); other rejections surface the server's validation reason category.
  if (error instanceof ConnectError) {
    switch (error.code) {
      case Code.FailedPrecondition:
        return "This instance is already set up.";
      case Code.PermissionDenied:
        // One message for every token refusal: the server does not reveal whether the token was wrong, expired, rotated or used (ADR-0052).
        return "The setup token is missing, wrong, expired or already used. Copy the latest token from the server log or setup token file; restarting the server issues a new one.";
      case Code.InvalidArgument:
        return "Please double-check the email and display name — and note the password needs at least 15 characters.";
      case Code.ResourceExhausted:
        return "Too many attempts — wait a moment and retry.";
    }
  }
  return "Something went wrong. Please retry.";
}
