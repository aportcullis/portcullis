import { z } from "zod";

/** Reports a rejected boundary without retaining source values or validation issues. */
export class ValidationError extends Error {
  constructor() {
    super("The response is invalid.");
    this.name = "ValidationError";
  }
}

/** Parses a boundary value into its schema output type. */
export function parseValidatedValue<Schema extends z.ZodType>(schema: Schema, input: unknown): z.output<Schema> {
  try {
    const result = schema.safeParse(input);
    if (result.success) return result.data;
  } catch {
    throw new ValidationError();
  }
  throw new ValidationError();
}

/** Recognizes sanitized failures from a runtime validation boundary. */
export function isValidationError(error: unknown): error is ValidationError {
  return error instanceof ValidationError;
}
