/** Form fields deliberately cover a subset of the restricted CEL contract. */
export type ConditionChoices = { title: string; startsAt: string; expiresAt: string; resourceType: string; resourceID: string };

/** Resource kinds supported by the versioned server environment. */
export const conditionResourceTypes = ["organization", "connection", "policy", "request", "execution", "result", "user", "role", "binding", "settings", "audit"];

/** Canonical resource identifiers cannot inject expression syntax. */
export function isManagementID(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value);
}

/** Refuses ambiguous local times and invalid dates before generating timestamp literals. */
function normalizeConditionTime(value: string): string {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) throw new Error("Use an ISO timestamp with seconds and an explicit time zone.");
  const year = Number(value.slice(0, 4)); const month = Number(value.slice(5, 7)); const day = Number(value.slice(8, 10));
  const calendar = new Date(0); calendar.setUTCFullYear(year, month - 1, day);
  if (year < 1 || calendar.getUTCFullYear() !== year || calendar.getUTCMonth() !== month - 1 || calendar.getUTCDate() !== day) throw new Error("Invalid calendar date.");
  const timestamp = new Date(value);
  if (!Number.isFinite(timestamp.getTime())) throw new Error("Invalid condition time.");
  return timestamp.toISOString();
}

/** Produces a complete source contract or refuses incomplete choices. */
export function buildCondition(choices: ConditionChoices) {
  if (choices.title.trim() === "") throw new Error("A condition title is required.");
  const expressions: string[] = [];
  const startsAt = choices.startsAt === "" ? "" : normalizeConditionTime(choices.startsAt);
  const expiresAt = choices.expiresAt === "" ? "" : normalizeConditionTime(choices.expiresAt);
  if (startsAt !== "" && expiresAt !== "" && startsAt >= expiresAt) throw new Error("Expiry must follow the start time.");
  if (startsAt !== "") expressions.push(`request.time >= timestamp(${JSON.stringify(startsAt)})`);
  if (expiresAt !== "") expressions.push(`request.time < timestamp(${JSON.stringify(expiresAt)})`);
  if (choices.resourceType !== "") {
    if (!conditionResourceTypes.includes(choices.resourceType)) throw new Error("Unsupported resource type.");
    expressions.push(`resource.type == ${JSON.stringify(choices.resourceType)}`);
  }
  if (choices.resourceID !== "") {
    if (!isManagementID(choices.resourceID)) throw new Error("Use a canonical resource UUID.");
    expressions.push(`resource.id == ${JSON.stringify(choices.resourceID)}`);
  }
  if (expressions.length === 0) throw new Error("Select at least one time or resource restriction.");
  return { title: choices.title.trim(), description: "", expression: expressions.join(" && "), languageVersion: "cel-v1", environmentVersion: "portcullis-v1" };
}
