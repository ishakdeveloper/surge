import { Input as InputPrimitive } from "@base-ui/react/input";
import * as React from "react";

import { cn } from "@/lib/utils";

/**
 * A field is a grey tile, not a box: no border until it has focus, when it
 * turns white and takes an ink ring inside its edge — the same lift the
 * rider's stops make when they are edited. Invalid is the refused red.
 */
function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <InputPrimitive
      type={type}
      data-slot="input"
      className={cn(
        "h-12 w-full min-w-0 rounded-xl border-0 bg-tile px-4 text-base text-foreground transition-[background-color,box-shadow] duration-150 ease-out outline-none placeholder:text-muted-foreground file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-semibold file:text-foreground hover:bg-tile-hover focus-visible:bg-card focus-visible:shadow-[inset_0_0_0_2px_var(--foreground)] focus-visible:outline-none disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:bg-destructive/5 aria-invalid:shadow-[inset_0_0_0_2px_var(--destructive)]",
        className,
      )}
      {...props}
    />
  );
}

export { Input };
