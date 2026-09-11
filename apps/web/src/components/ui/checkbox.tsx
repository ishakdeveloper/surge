import { Checkbox as CheckboxPrimitive } from "@base-ui/react/checkbox";

import { cn } from "@/lib/utils";
import { CheckIcon } from "lucide-react";

/**
 * A soft grey square ringed faintly in ink; checked, it fills with ink and
 * carries a yellow check — the chosen class's radio mark, squared.
 */
function Checkbox({ className, ...props }: CheckboxPrimitive.Root.Props) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        "peer relative flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-md border-0 bg-tile text-primary shadow-[inset_0_0_0_1.5px_rgb(26_26_26/0.25)] transition-[background-color,box-shadow] duration-150 ease-out after:absolute after:-inset-x-3 after:-inset-y-2 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:shadow-[inset_0_0_0_2px_var(--destructive)] data-checked:bg-foreground data-checked:shadow-none",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="grid place-content-center text-current [&>svg]:size-3.5"
      >
        <CheckIcon strokeWidth={3} />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

export { Checkbox };
