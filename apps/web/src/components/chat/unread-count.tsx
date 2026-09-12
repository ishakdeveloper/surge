/**
 * How many messages are waiting, as a yellow count: reading them is the next
 * thing to do. Hidden from assistive tech, so the row or tile it sits in says
 * the number in words.
 */
export const UnreadCount = (props: { readonly count: number; }) => (
  <span
    aria-hidden
    className="grid h-6 min-w-6 shrink-0 place-items-center rounded-full bg-primary px-1.5 text-xs font-bold text-primary-foreground tabular-nums motion-safe:animate-pop-in"
  >
    {props.count > 99 ? "99+" : props.count}
  </span>
);

/** The same number in words, for the row or tile that shows the count. */
export const unreadWords = (count: number): string =>
  `${count} new ${count === 1 ? "message" : "messages"}`;
