import { z } from "zod";

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type {
  AccessRequest,
  AccessRequestPayload,
  GetAccessRequestResponse,
  ListAccessRequestsResponse,
  RequestActor,
  RequestApproval,
  TypedParam,
} from "@/gen/portcullis/v1/access_requests_pb";
import { AccessRequestState } from "@/gen/portcullis/v1/access_requests_pb";
import { parseValidatedValue, ValidationError } from "@/shared/lib/validation";

export type ValidatedAccessRequest = AccessRequest & { requester: RequestActor };
export type ValidatedAccessRequestDetails = GetAccessRequestResponse & { request: ValidatedAccessRequest };
export type ValidatedAccessRequestList = ListAccessRequestsResponse & { items: ValidatedAccessRequest[] };

const maxSignedInteger = 9223372036854775807n;
const unsignedIntegerSchema = z.number().int().min(0).max(4294967295);
const timestampSchema: z.ZodType<Timestamp> = z.object({
  $typeName: z.literal("google.protobuf.Timestamp"),
  seconds: z.bigint().min(-62135596800n).max(253402300799n),
  nanos: z.number().int().min(0).max(999999999),
});
const requestActorSchema: z.ZodType<RequestActor> = z.object({
  $typeName: z.literal("portcullis.v1.RequestActor"),
  id: z.string().min(1),
  email: z.string(),
  displayName: z.string(),
});
const requestApprovalSchema: z.ZodType<RequestApproval> = z.object({
  $typeName: z.literal("portcullis.v1.RequestApproval"),
  approver: requestActorSchema.optional(),
  decision: z.enum(["approved", "rejected"]),
  reason: z.string(),
  decidedAt: timestampSchema.optional(),
  valid: z.boolean(),
});
const requestStateSchema = z.union([
  z.literal(AccessRequestState.DRAFT),
  z.literal(AccessRequestState.PENDING),
  z.literal(AccessRequestState.APPROVED),
  z.literal(AccessRequestState.REJECTED),
  z.literal(AccessRequestState.EXPIRED),
  z.literal(AccessRequestState.CANCELLED),
  z.literal(AccessRequestState.EXECUTING),
  z.literal(AccessRequestState.SUCCEEDED),
  z.literal(AccessRequestState.FAILED),
  z.literal(AccessRequestState.OUTCOME_UNKNOWN),
]);
const accessRequestSchema: z.ZodType<ValidatedAccessRequest> = z.object({
  $typeName: z.literal("portcullis.v1.AccessRequest"),
  id: z.string().min(1),
  connectionId: z.string().min(1),
  connectionName: z.string(),
  requester: requestActorSchema,
  state: requestStateSchema,
  effectiveState: requestStateSchema,
  stateReason: z.enum(["", "policy_changed", "connection_changed", "connection_archived", "approval_invalidated", "ttl_expired"]),
  redactedSql: z.string(),
  statementClass: z.enum(["", "read", "write", "ddl"]),
  policyVersion: z.bigint().min(0n).max(maxSignedInteger),
  requiredApprovals: unsignedIntegerSchema,
  approvals: z.array(requestApprovalSchema),
  validApprovals: unsignedIntegerSchema,
  submittedAt: timestampSchema.optional(),
  expiresAt: timestampSchema.optional(),
  version: z.bigint().min(1n).max(maxSignedInteger),
  createdAt: timestampSchema.optional(),
  updatedAt: timestampSchema.optional(),
  connectionDbType: z.string(),
  connectionFingerprint: z.string(),
  connectionConfigVersion: z.bigint().min(0n).max(maxSignedInteger),
  title: z.string(),
});
const typedParameterSchema: z.ZodType<TypedParam> = z.object({
  $typeName: z.literal("portcullis.v1.TypedParam"),
  name: z.string().min(1),
  type: z.enum(["string", "integer", "decimal", "boolean", "date", "timestamp", "uuid", "null"]),
  value: z.string(),
});
const requestPayloadSchema: z.ZodType<AccessRequestPayload> = z.object({
  $typeName: z.literal("portcullis.v1.AccessRequestPayload"),
  sql: z.string(),
  params: z.array(typedParameterSchema),
  title: z.string(),
  body: z.string(),
});
const requestDetailsSchema: z.ZodType<ValidatedAccessRequestDetails> = z.object({
  $typeName: z.literal("portcullis.v1.GetAccessRequestResponse"),
  request: accessRequestSchema,
  payload: requestPayloadSchema.optional(),
});
const requestListSchema: z.ZodType<ValidatedAccessRequestList> = z.object({
  $typeName: z.literal("portcullis.v1.ListAccessRequestsResponse"),
  items: z.array(accessRequestSchema),
  page: z.number().int().min(1).max(4294967295),
  pageSize: z.union([z.literal(10), z.literal(20), z.literal(50), z.literal(100)]),
  totalCount: z.bigint().min(0n).max(maxSignedInteger),
  totalPages: unsignedIntegerSchema,
});

export class AccessRequestValidationError extends ValidationError {
  constructor() {
    super();
    this.name = "AccessRequestValidationError";
    this.message = "The access request response is invalid.";
  }
}

export function parseAccessRequestList(input: unknown): ValidatedAccessRequestList {
  try {
    return parseValidatedValue(requestListSchema, input);
  } catch {
    throw new AccessRequestValidationError();
  }
}

export function parseAccessRequestDetails(input: unknown): ValidatedAccessRequestDetails {
  try {
    return parseValidatedValue(requestDetailsSchema, input);
  } catch {
    throw new AccessRequestValidationError();
  }
}
