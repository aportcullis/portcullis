import type { ManagementKind } from "@/entities/administration/api";
import { ManagementWorkspace } from "@/features/administration/ManagementWorkspace";

/** Assembles independent management routes in one lazy workflow entry. */
export default function AdministrationPage(props: { kind: ManagementKind }) {
  return <ManagementWorkspace kind={props.kind} />;
}
