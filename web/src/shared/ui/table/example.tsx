import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell, TableCaption } from "@/shared/ui/table";

/** Demonstrates the public table API without application services. */
export function Example() {
  return (<Table><TableCaption>Sample items</TableCaption><TableHeader><TableRow><TableHead scope="col">Name</TableHead></TableRow></TableHeader><TableBody><TableRow><TableCell>Example</TableCell></TableRow></TableBody></Table>);
}
