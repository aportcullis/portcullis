import { AccessRequestValidationError } from "@/shared/api/accessRequestValidation";
import { errorMessage } from "@/shared/api/errors";

export function formatRequestMutationError(error: unknown): string {
  return error instanceof AccessRequestValidationError
    ? "The saved request response is invalid. The draft may have been saved. Check your requests before retrying."
    : errorMessage(error);
}
