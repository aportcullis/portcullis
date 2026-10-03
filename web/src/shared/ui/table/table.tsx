import type { Component, ComponentProps } from "solid-js"
import { splitProps } from "solid-js"

import { cn } from "@/shared/lib/utils"

// Vendored from solid-ui (stefan-karger/solid-ui), adapted: plain styled HTML table primitives — sorting/virtualization (TanStack) is reserved for the result grid (PRD §7.1), not needed for small admin lists.

const Table: Component<ComponentProps<"table">> = (props) => {
  const [local, others] = splitProps(props, ["class"])
  return (
    <div class="relative w-full min-w-0 overflow-auto">
      <table class={cn("w-full caption-bottom text-sm", local.class)} {...others} />
    </div>
  )
}

const TableHeader: Component<ComponentProps<"thead">> = (props) => {
  const [local, others] = splitProps(props, ["class"])
  return <thead class={cn("bg-muted/50 [&_tr]:border-b", local.class)} {...others} />
}

const TableBody: Component<ComponentProps<"tbody">> = (props) => {
  const [local, others] = splitProps(props, ["class"])
  return <tbody class={cn("[&_tr:last-child]:border-0", local.class)} {...others} />
}

const TableRow: Component<ComponentProps<"tr">> = (props) => {
  const [local, others] = splitProps(props, ["class"])
  return (
    <tr
      class={cn("border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted", local.class)}
      {...others}
    />
  )
}

const TableHead: Component<ComponentProps<"th">> = (props) => {
  const [local, others] = splitProps(props, ["class"])
  return (
    <th
      class={cn(
        "h-12 px-4 text-left align-middle font-medium text-muted-foreground has-[[role=checkbox]]:pr-0",
        local.class
      )}
      {...others}
    />
  )
}

const TableCell: Component<ComponentProps<"td">> = (props) => {
  const [local, others] = splitProps(props, ["class"])
  return (
    <td class={cn("p-4 align-middle has-[[role=checkbox]]:pr-0", local.class)} {...others} />
  )
}

const TableCaption: Component<ComponentProps<"caption">> = (props) => {
  const [local, others] = splitProps(props, ["class"])
  return <caption class={cn("mt-4 text-sm text-muted-foreground", local.class)} {...others} />
}

export { Table, TableHeader, TableBody, TableRow, TableHead, TableCell, TableCaption }
