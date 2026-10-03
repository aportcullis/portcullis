import { Alert, AlertTitle, AlertDescription } from "@/shared/ui/alert";

/** Demonstrates the public alert API without application services. */
export function Example() {
  return (<Alert><AlertTitle>Check your input</AlertTitle><AlertDescription>Provide a descriptive title.</AlertDescription></Alert>);
}
