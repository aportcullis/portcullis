import type { ManagedBinding, ManagedRole, ManagedUser, AdministrationPageInfo } from "@/gen/portcullis/v1/administration_pb";
import { bindingsClient, rolesClient, usersClient } from "@/shared/api/client";

export type ManagementKind = "users" | "roles" | "bindings";
export type ManagementRow = { id: string; label: string; status: string; version: bigint; userID: string; isSystem: boolean; bindingCount: bigint; scope: string };
export type ManagementList = { rows: ManagementRow[]; pagination: AdministrationPageInfo | undefined };
export type ManagementDetail = { kind: "users"; value: ManagedUser } | { kind: "roles"; value: ManagedRole } | { kind: "bindings"; value: ManagedBinding };

/** Loads only the selected independently authorized collection. */
export async function listManagement(kind: ManagementKind, page: number, pageSize: number, expectedRevision: bigint): Promise<ManagementList> {
  const pagination = { page, pageSize, expectedRevision };
  if (kind === "users") {
    const result = await usersClient.list({ pagination });
    return { pagination: result.pagination, rows: result.users.map(user => ({ id: user.id, label: user.email, status: user.status, version: 0n, userID: user.id, isSystem: false, bindingCount: 0n, scope: "" })) };
  }
  if (kind === "roles") {
    const result = await rolesClient.list({ pagination });
    return { pagination: result.pagination, rows: result.roles.map(role => ({ id: role.id, label: role.name, status: role.isSystem ? "System" : "Custom", version: role.version, userID: "", isSystem: role.isSystem, bindingCount: role.bindingCount, scope: "" })) };
  }
  const result = await bindingsClient.list({ pagination });
  return { pagination: result.pagination, rows: result.bindings.map(binding => ({ id: binding.id, label: `${binding.userId} → ${binding.roleId}`, status: binding.revoked ? "Revoked" : binding.hasCondition ? "Conditional" : "Unconditional", version: binding.version, userID: binding.userId, isSystem: false, bindingCount: 0n, scope: `${binding.scope?.type ?? ""}${binding.scope?.connectionId ? ` · ${binding.scope.connectionId}` : ""}` })) };
}

/** Fetches protected detail through its specific Get operation, never a list cache. */
export async function getManagement(kind: ManagementKind, id: string): Promise<ManagementDetail> {
  if (kind === "users") { const result = await usersClient.get({ id }); if (result.user?.user) return { kind, value: result.user }; }
  else if (kind === "roles") { const result = await rolesClient.get({ id }); if (result.role?.summary) return { kind, value: result.role }; }
  else { const result = await bindingsClient.get({ id }); if (result.binding?.summary) return { kind, value: result.binding }; }
  throw new Error("Missing authorized detail");
}

/** Issues a grantless account and its fresh setup receipt. */
export const createManagedUser = (input: Parameters<typeof usersClient.create>[0]) => usersClient.create(input);
/** Changes account state without importing detail read authority. */
export const changeManagedUserStatus = async (id: string, enable: boolean) => enable ? usersClient.enable({ id }) : usersClient.disable({ id });
/** Issues a replacement token only through the eligible-account operation. */
export const issueManagedSetup = (id: string) => usersClient.issueSetupLink({ id });
/** Creates a reusable permission definition. */
export const createManagedRole = (input: Parameters<typeof rolesClient.create>[0]) => rolesClient.create(input);
/** Changes a definition using the caller's displayed version. */
export const updateManagedRole = (input: Parameters<typeof rolesClient.update>[0]) => rolesClient.update(input);
/** Removes an unreferenced custom definition with optimistic concurrency. */
export const deleteManagedRole = (id: string, expectedVersion: bigint) => rolesClient.delete({ id, expectedVersion });
/** Loads the current vocabulary under separate roles.list authority. */
export const listManagedPermissions = () => rolesClient.listPermissions({});
/** Validates generated source with the same restricted server preparer as writes. */
export const validateManagedCondition = (condition: Parameters<typeof bindingsClient.validateCondition>[0]["condition"]) => bindingsClient.validateCondition({ condition });
/** Creates an independently scoped, optionally conditional grant. */
export const createManagedBinding = (input: Parameters<typeof bindingsClient.create>[0]) => bindingsClient.create(input);
/** Changes a grant without altering omitted source data. */
export const updateManagedBinding = (input: Parameters<typeof bindingsClient.update>[0]) => bindingsClient.update(input);
/** Revokes a displayed grant without first reading its protected source. */
export const revokeManagedBinding = (id: string, expectedVersion: bigint) => bindingsClient.revoke({ id, expectedVersion });
