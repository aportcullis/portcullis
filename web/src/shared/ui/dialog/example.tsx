import { Dialog, DialogTrigger, DialogContent, DialogHeader, DialogFooter, DialogTitle, DialogDescription } from "@/shared/ui/dialog";

/** Demonstrates the public dialog API without application services. */
export function Example() {
  return (<Dialog><DialogTrigger>Review removal</DialogTrigger><DialogContent><DialogHeader><DialogTitle>Remove this item?</DialogTitle><DialogDescription>Review the impact before confirming.</DialogDescription></DialogHeader><DialogFooter>No action is performed in this example.</DialogFooter></DialogContent></Dialog>);
}
