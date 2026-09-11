import * as React from "react";

import { cn } from "@/lib/utils";

/** The input's grey tile, grown to hold paragraphs. */
function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        "flex field-sizing-content min-h-24 w-full rounded-xl border-0 bg-tile px-4 py-3 text-base text-foreground transition-[background-color,box-shadow] duration-150 ease-out outline-none placeholder:text-muted-foreground hover:bg-tile-hover focus-visible:bg-card focus-visible:shadow-[inset_0_0_0_2px_var(--foreground)] focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:bg-destructive/5 aria-invalid:shadow-[inset_0_0_0_2px_var(--destructive)]",
        className,
      )}
      {...props}
    />
  );
}

export { Textarea };
