import { nativeControlVariants } from "@/shared/ui/control";

/** Demonstrates native labeling and compact sizing without application services. */
export function Example() {
  return (
    <label>
      Page size
      <select class={nativeControlVariants({ size: "sm" })}>
        <option>20</option>
      </select>
    </label>
  );
}
