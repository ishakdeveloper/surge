import { cn } from "@/lib/utils.js";

/**
 * Surge's mark: a sign-yellow tile with the arrow every wayfinding sign points
 * with. Decorative beside the wordmark, so hidden from assistive tech.
 */
export const SurgeMark = (props: { readonly className?: string; }) => (
  <svg viewBox="0 0 28 28" className={cn("size-7 shrink-0", props.className)} aria-hidden>
    <rect width="28" height="28" rx="8" fill="var(--primary)" />
    <path
      d="M9 19 19 9m0 0h-8.5M19 9v8.5"
      fill="none"
      stroke="var(--primary-foreground)"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);
