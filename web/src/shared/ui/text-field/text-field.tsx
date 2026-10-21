import type { ValidComponent } from "solid-js";
import { mergeProps, splitProps } from "solid-js";

import type { PolymorphicProps } from "@kobalte/core";
import * as TextFieldPrimitive from "@kobalte/core/text-field";
import { cva } from "class-variance-authority";

import { nativeControlVariants, type ControlSize } from "@/shared/ui/control";

import { cn } from "@/shared/lib/utils";

type TextFieldRootProps<T extends ValidComponent = "div"> =
  TextFieldPrimitive.TextFieldRootProps<T> & {
    class?: string | undefined;
  };

// The text-field parts render their default elements only: not forwarding Kobalte's polymorphic `as` keeps their props typed without assertions.
const TextField = (props: PolymorphicProps<"div", TextFieldRootProps>) => {
  const [local, others] = splitProps(props, ["class"]);
  return <TextFieldPrimitive.Root class={cn("flex flex-col gap-1", local.class)} {...others} />;
};

/** Enumerates the native input kinds supported by the owned text-field wrapper. */
export type TextFieldInputType =
  | "button"
  | "checkbox"
  | "color"
  | "date"
  | "datetime-local"
  | "email"
  | "file"
  | "hidden"
  | "image"
  | "month"
  | "number"
  | "password"
  | "radio"
  | "range"
  | "reset"
  | "search"
  | "submit"
  | "tel"
  | "text"
  | "time"
  | "url"
  | "week";

export type TextFieldInputProps<T extends ValidComponent = "input"> =
  TextFieldPrimitive.TextFieldInputProps<T> & {
    class?: string | undefined;
    controlSize?: ControlSize | null;
    type?: TextFieldInputType;
  };

const TextFieldInput = (rawProps: PolymorphicProps<"input", TextFieldInputProps>) => {
  const props = mergeProps({ type: "text" }, rawProps);
  const [local, others] = splitProps(props, ["type", "class", "controlSize"]);
  return (
    <TextFieldPrimitive.Input
      type={local.type}
      class={cn(
        nativeControlVariants({ size: local.controlSize }),
        "flex w-full file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground data-invalid:border-destructive-text data-invalid:text-destructive-text",
        local.class,
      )}
      {...others}
    />
  );
};

type TextFieldTextAreaProps<T extends ValidComponent = "textarea"> =
  TextFieldPrimitive.TextFieldTextAreaProps<T> & { class?: string | undefined };

const TextFieldTextArea = (props: PolymorphicProps<"textarea", TextFieldTextAreaProps>) => {
  const [local, others] = splitProps(props, ["class"]);
  return (
    <TextFieldPrimitive.TextArea
      class={cn(
        "flex min-h-20 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50 data-invalid:border-destructive-text data-invalid:text-destructive-text",
        local.class,
      )}
      {...others}
    />
  );
};

const labelVariants = cva(
  "text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70",
  {
    variants: {
      variant: {
        label: "data-invalid:text-destructive-text",
        description: "font-normal text-muted-foreground",
        error: "text-xs text-destructive-text",
      },
    },
    defaultVariants: {
      variant: "label",
    },
  },
);

type TextFieldLabelProps<T extends ValidComponent = "label"> =
  TextFieldPrimitive.TextFieldLabelProps<T> & { class?: string | undefined };

const TextFieldLabel = (props: PolymorphicProps<"label", TextFieldLabelProps>) => {
  const [local, others] = splitProps(props, ["class"]);
  return <TextFieldPrimitive.Label class={cn(labelVariants(), local.class)} {...others} />;
};

type TextFieldDescriptionProps<T extends ValidComponent = "div"> =
  TextFieldPrimitive.TextFieldDescriptionProps<T> & {
    class?: string | undefined;
  };

const TextFieldDescription = (props: PolymorphicProps<"div", TextFieldDescriptionProps>) => {
  const [local, others] = splitProps(props, ["class"]);
  return (
    <TextFieldPrimitive.Description
      class={cn(labelVariants({ variant: "description" }), local.class)}
      {...others}
    />
  );
};

type TextFieldErrorMessageProps<T extends ValidComponent = "div"> =
  TextFieldPrimitive.TextFieldErrorMessageProps<T> & {
    class?: string | undefined;
  };

const TextFieldErrorMessage = (props: PolymorphicProps<"div", TextFieldErrorMessageProps>) => {
  const [local, others] = splitProps(props, ["class"]);
  return (
    <TextFieldPrimitive.ErrorMessage
      class={cn(labelVariants({ variant: "error" }), local.class)}
      {...others}
    />
  );
};

export {
  TextField,
  TextFieldInput,
  TextFieldTextArea,
  TextFieldLabel,
  TextFieldDescription,
  TextFieldErrorMessage,
};
