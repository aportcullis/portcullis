import { createMemo, For } from "solid-js";

import { cn } from "@/shared/lib/utils";
import { createIdenticon } from "@/shared/ui/avatar/identicon";

export type AvatarProps = { seed: string; label: string; class?: string };

/** Displays a stable local profile image with an accessible label. */
export function Avatar(props: AvatarProps) {
  const identicon = createMemo(() => createIdenticon(props.seed));
  return (
    <svg role="img" aria-label={props.label} viewBox="0 0 7 7" class={cn("inline-block size-9 shrink-0 rounded-lg border border-border bg-muted", props.class)}>
      <For each={identicon().cells}>
        {(cell) => <rect x={cell.x + 1} y={cell.y + 1} width="1" height="1" fill={identicon().color} />}
      </For>
    </svg>
  );
}
