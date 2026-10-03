import { z } from 'zod';

/** Validates padded protobuf base64 without browser-only atob. */
export const base64Text = z.string().refine(value => value.length % 4 === 0 && /^[A-Za-z0-9+/]*={0,2}$/.test(value), 'Invalid base64');

const text = z.string().default('');
const count = z.int().nonnegative().default(0);
const integerText = z.string().regex(/^-?\d+$/);
const unsignedText = z.string().regex(/^\d+$/);
const state = z.enum([
  'ACCESS_REQUEST_STATE_UNSPECIFIED', 'ACCESS_REQUEST_STATE_DRAFT', 'ACCESS_REQUEST_STATE_PENDING',
  'ACCESS_REQUEST_STATE_APPROVED', 'ACCESS_REQUEST_STATE_REJECTED', 'ACCESS_REQUEST_STATE_EXPIRED',
  'ACCESS_REQUEST_STATE_CANCELLED', 'ACCESS_REQUEST_STATE_EXECUTING', 'ACCESS_REQUEST_STATE_SUCCEEDED',
  'ACCESS_REQUEST_STATE_FAILED', 'ACCESS_REQUEST_STATE_OUTCOME_UNKNOWN',
]);
const timestamp = z.iso.datetime({ offset: true }).optional();
const actorView = z.object({ id: z.string().min(1), email: text, displayName: text });
const param = z.strictObject({ name: z.string(), type: z.enum(['string', 'integer', 'decimal', 'boolean', 'date', 'timestamp', 'uuid', 'null']), value: z.string() });
const requestView = z.object({
  id: z.string().min(1), version: unsignedText, state, effectiveState: state.optional(),
  connectionId: text, connectionName: text, requester: actorView.optional(), stateReason: text,
  redactedSql: text, statementClass: text, policyVersion: unsignedText.default('0'),
  requiredApprovals: count, validApprovals: count,
  approvals: z.array(z.object({ approver: actorView.optional(), decision: z.enum(['approved', 'rejected']), reason: text, decidedAt: timestamp, valid: z.boolean().default(false) })).default([]),
  submittedAt: timestamp, expiresAt: timestamp, createdAt: timestamp, updatedAt: timestamp,
  connectionDbType: text, connectionFingerprint: text, connectionConfigVersion: unsignedText.default('0'), title: text,
});
const requestResponse = z.object({ request: requestView });
export const executionView = z.object({
  state, rowsAffected: integerText.default('0'), rowCount: unsignedText.default('0'),
  byteCount: unsignedText.default('0'), durationMs: unsignedText.default('0'),
  resultAvailable: z.boolean().default(false), truncated: z.boolean().default(false), resultExpiresAt: timestamp,
});
const cell = z.union([
  z.strictObject({ isNull: z.literal(true) }), z.strictObject({ stringValue: z.string() }),
  z.strictObject({ boolValue: z.boolean() }), z.strictObject({ intValue: integerText }),
  z.strictObject({ decimalValue: z.string() }),
  z.strictObject({ doubleValue: z.union([z.number(), z.enum(['NaN', 'Infinity', '-Infinity'])]) }),
  z.strictObject({ bytesValue: base64Text }), z.strictObject({ temporalValue: z.string() }),
]);
const requestID = z.strictObject({ id: z.string().min(1) });
export const executionRequest = z.strictObject({ requestId: z.string().min(1) });
const empty = z.strictObject({});
const logicalType = z.enum(['LOGICAL_TYPE_UNSPECIFIED', 'LOGICAL_TYPE_STRING', 'LOGICAL_TYPE_BOOL', 'LOGICAL_TYPE_INT', 'LOGICAL_TYPE_DECIMAL', 'LOGICAL_TYPE_FLOAT', 'LOGICAL_TYPE_BYTES', 'LOGICAL_TYPE_DATE', 'LOGICAL_TYPE_TIME', 'LOGICAL_TYPE_TIMESTAMP', 'LOGICAL_TYPE_TIMESTAMPTZ', 'LOGICAL_TYPE_JSON', 'LOGICAL_TYPE_UUID', 'LOGICAL_TYPE_ARRAY', 'LOGICAL_TYPE_UNKNOWN']);

/** Validates the Connect JSON contracts used by the measured journeys. */
export const rpcSchemas = {
  'Auth.Me': { input: empty, output: z.object({ user: z.object({ id: z.string().min(1), email: text, displayName: text, status: z.enum(['active', 'disabled']) }), permissions: z.array(z.string()).default([]), roleName: text }) },
  'AccessRequests.List': {
    input: z.strictObject({ page: z.int().nonnegative(), pageSize: z.int().positive(), descending: z.boolean().optional(), state: z.string().optional() }),
    output: z.object({ items: z.array(requestView).default([]), page: count, pageSize: count, totalCount: unsignedText.default('0'), totalPages: count }),
  },
  'AccessRequests.ListRequestableConnections': { input: empty, output: z.object({ connections: z.array(z.object({ id: z.string().min(1), displayName: text, dbType: text, environment: text })).default([]) }) },
  'AccessRequests.Create': { input: z.strictObject({ connectionId: z.string().min(1), sql: z.string(), params: z.array(param), title: z.string().optional(), body: z.string().optional() }), output: requestResponse },
  'AccessRequests.Submit': { input: requestID.extend({ expectedVersion: unsignedText }), output: requestResponse },
  'AccessRequests.Get': { input: requestID, output: requestResponse.extend({ payload: z.object({ sql: text, params: z.array(param).default([]), title: text, body: text }).optional() }) },
  'AccessRequests.Approve': { input: requestID.extend({ reason: z.string().optional() }), output: requestResponse },
  'AccessRequests.Cancel': { input: requestID, output: requestResponse },
  'QueryExecutions.Execute': { input: executionRequest, output: executionView },
  'QueryExecutions.Get': { input: executionRequest, output: executionView },
  'QueryExecutions.Cancel': { input: executionRequest, output: empty },
  'QueryExecutions.GetResult': {
    input: z.strictObject({ requestId: z.string().min(1), page: z.int().nonnegative(), pageSize: z.int().positive(), sortColumn: z.int().nonnegative().optional(), descending: z.boolean().optional(), filterColumn: z.int().nonnegative().optional(), filter: z.string().optional() }),
    output: z.object({ totalCount: unsignedText.default('0'), truncated: z.boolean().default(false),
      rows: z.array(z.object({ cells: z.array(cell).default([]) })).default([]),
      columns: z.array(z.object({ name: text, logicalType: logicalType.default('LOGICAL_TYPE_UNSPECIFIED'), dbTypeName: text, nullable: z.boolean().default(false) })).default([]),
      page: count, pageSize: count, totalPages: count, expiresAt: timestamp }),
  },
};

export const actorSchema = z.strictObject({ session: z.string().min(1), csrf: z.string().min(1) });
export const fixtureSchema = z.strictObject({ connectionId: z.string().min(1), requester: actorSchema, approver: actorSchema.optional() });
export type Actor = z.output<typeof actorSchema>;
export type Fixture = z.output<typeof fixtureSchema>;
export type RPCName = keyof typeof rpcSchemas;
export type RPCs = { [K in RPCName]: { input: z.input<typeof rpcSchemas[K]['input']>; output: z.output<typeof rpcSchemas[K]['output']> } };
