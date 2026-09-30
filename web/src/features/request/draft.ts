import type { AccessRequestPayload, TypedParam } from "@/gen/portcullis/v1/access_requests_pb";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import type { ParamRow, ParamType } from "@/entities/request/model";
import { paramTypes } from "@/entities/request/model";

/** Holds editable SQL and typed parameters for a request draft. */
export type RequestDraft = {
  sql: string;
  params: ParamRow[];
};

/** Creates a request draft with no SQL or parameters. */
export function createEmptyRequestDraft(): RequestDraft {
  return { sql: "", params: [] };
}

/** Checks whether a parameter type is supported by the form. */
function isSupportedParameterType(value: string): value is ParamType {
  return paramTypes.some((parameterType) => parameterType === value);
}

/** Builds a draft from a decrypted payload, defaulting unknown parameter types to string. */
export function createRequestDraftFromPayload(payload: AccessRequestPayload): RequestDraft {
  return {
    sql: payload.sql,
    params: payload.params.map((parameter) => ({
      name: parameter.name,
      type: isSupportedParameterType(parameter.type) ? parameter.type : "string",
      value: parameter.value,
    })),
  };
}

/** Appends an empty string parameter to the draft. */
export function appendDraftParameter(draft: RequestDraft): RequestDraft {
  return { ...draft, params: [...draft.params, { name: "", type: "string", value: "" }] };
}

/** Removes the parameter at the selected index. */
export function removeDraftParameter(draft: RequestDraft, index: number): RequestDraft {
  return { ...draft, params: draft.params.filter((_, parameterIdx) => parameterIdx !== index) };
}

/** Updates the parameter at the selected index. */
export function updateDraftParameter(draft: RequestDraft, index: number, patch: Partial<ParamRow>): RequestDraft {
  return {
    ...draft,
    params: draft.params.map((parameter, parameterIdx) => (parameterIdx === index ? { ...parameter, ...patch } : parameter)),
  };
}

/** Checks whether the selected connection remains available after a target refresh. */
export function isSelectedTargetAvailable(targets: readonly { id: string }[], selected: string): boolean {
  return selected === "" || targets.some((target) => target.id === selected);
}

// An unsaved form may choose another target; a saved draft’s target is immutable and needs an exit when unavailable.
export type TargetLoss = "repick" | "unsubmittable";

export function resolveUnavailableTargetAction(saved: boolean): TargetLoss {
  return saved ? "unsubmittable" : "repick";
}

// Offer withdrawal only if the saved request remains a draft; archive already cancels it atomically.
export type LostTargetOutcome = "already-settled" | "still-cancellable";

export function resolveUnavailableTargetRequestState(state: AccessRequestState): LostTargetOutcome {
  return state === AccessRequestState.DRAFT ? "still-cancellable" : "already-settled";
}

/** Reports whether the parameter type accepts no value. */
export function isParameterValueDisabled(type: ParamType): boolean {
  return type === "null";
}

/** Returns the first draft-validation error, or an empty string when valid. */
export function validateRequestDraft(draft: RequestDraft): string {
  if (draft.sql.trim() === "") {
    return "Enter a SQL statement.";
  }
  const seen = new Set<string>();
  for (const parameter of draft.params) {
    if (parameter.name.trim() === "") {
      return "Every parameter needs a name.";
    }
    if (seen.has(parameter.name)) {
      return `Parameter "${parameter.name}" is declared more than once.`;
    }
    seen.add(parameter.name);
    const problem = validateParameterValue(parameter);
    if (problem !== "") {
      return problem;
    }
  }
  return "";
}

function validateParameterValue(parameter: ParamRow): string {
  switch (parameter.type) {
    case "integer":
      return /^-?\d+$/.test(parameter.value) ? "" : `"${parameter.name}" must be a whole number.`;
    case "decimal":
      return /^-?\d+(\.\d+)?([eE][+-]?\d+)?$/.test(parameter.value) ? "" : `"${parameter.name}" must be a decimal.`;
    case "boolean":
      return parameter.value === "true" || parameter.value === "false" ? "" : `"${parameter.name}" must be true or false.`;
    case "uuid":
      return /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(parameter.value)
        ? ""
        : `"${parameter.name}" must be a UUID.`;
    default:
      return "";
  }
}

/** Converts draft parameters into API values, sending an empty value for null. */
export function toTypedRequestParameters(draft: RequestDraft): TypedParam[] {
  return draft.params.map((parameter) => ({
    $typeName: "portcullis.v1.TypedParam",
    name: parameter.name,
    type: parameter.type,
    value: isParameterValueDisabled(parameter.type) ? "" : parameter.value,
  }));
}
