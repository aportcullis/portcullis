import { PageHeader } from "@/shared/ui/PageHeader";

/** Demonstrates the public PageHeader API without application services. */
export function Example() {
  return (<PageHeader title="Overview" description="Explore available information." eyebrow="Workspace" actions={<button type="button">Refresh</button>} />);
}
