import { Card, CardHeader, CardFooter, CardTitle, CardDescription, CardContent } from "@/shared/ui/card";

/** Demonstrates the public card API without application services. */
export function Example() {
  return (<Card><CardHeader><CardTitle>Overview</CardTitle><CardDescription>Reusable content.</CardDescription></CardHeader><CardContent>Details</CardContent><CardFooter>Updated today</CardFooter></Card>);
}
