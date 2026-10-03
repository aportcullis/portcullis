import { TextField, TextFieldInput, TextFieldLabel, TextFieldDescription, TextFieldErrorMessage } from "@/shared/ui/text-field";

/** Demonstrates the public text-field API without application services. */
export function Example() {
  return (<TextField><TextFieldLabel>Title</TextFieldLabel><TextFieldInput /><TextFieldDescription>Describe the purpose.</TextFieldDescription><TextFieldErrorMessage>A title is required.</TextFieldErrorMessage></TextField>);
}
