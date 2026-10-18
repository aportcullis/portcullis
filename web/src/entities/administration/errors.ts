import { Code, ConnectError } from "@connectrpc/connect";

/** Classifies management failures without exposing raw condition or driver diagnostics. */
export function managementError(error: unknown): string {
  if (!(error instanceof ConnectError)) return "Request failed. Check your input and try again.";
  switch (error.code) {
    case Code.Unauthenticated: return "Your session ended. Sign in again.";
    case Code.PermissionDenied: return "Permission denied. Current server authority is required.";
    case Code.NotFound: return "Resource unavailable or you do not have permission to view it.";
    case Code.Aborted: return "This policy changed. Reload the current version before trying again.";
    case Code.FailedPrecondition: return "Change refused. Check the last administrator, system role, live bindings and account state.";
    case Code.InvalidArgument: return "Invalid input. Check identifiers, permissions and condition choices.";
    case Code.AlreadyExists: return "An account or role with these details already exists.";
    case Code.Unavailable: return "Management is currently unavailable. Reload to check the current state before trying again.";
    default: return "Request failed. Refresh to check the current state before trying again.";
  }
}

/** A refusal to current authority invalidates all protected route data. */
export function isManagementAdmissionFailure(error: unknown): boolean {
  return error instanceof ConnectError && [Code.Unauthenticated, Code.PermissionDenied, Code.NotFound].includes(error.code);
}
