import type { ManagementKind } from "@/entities/administration/api";
import { ManagementWorkspace } from "@/features/administration/ManagementWorkspace";

/** Defines the inputs owned by AdministrationPage. */
export type AdministrationPageProps = {
  kind: ManagementKind;
};

/** Assembles independent management routes in one lazy workflow entry. */
export default function AdministrationPage(props: AdministrationPageProps) {
  return <ManagementWorkspace kind={props.kind} />;
}
