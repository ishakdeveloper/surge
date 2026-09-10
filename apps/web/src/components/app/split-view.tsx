import type * as React from "react";

/**
 * The split panel from `knowledge/rules/dashboard-ui.md`: controls on the left,
 * the primary content on the right — for the rider and driver pages, the map.
 *
 * The aside scrolls on its own and the map fills what is left, so the page never
 * scrolls as a document. Stacked below `lg`, where a side-by-side map would be
 * too narrow to be worth drawing.
 */
export const SplitView = (
  props: { readonly aside: React.ReactNode; readonly children: React.ReactNode; },
) => (
  <div className="flex min-h-0 flex-1 flex-col gap-6 lg:flex-row">
    <aside className="flex w-full shrink-0 flex-col gap-5 lg:w-96 lg:overflow-y-auto">
      {props.aside}
    </aside>
    <div className="flex min-h-96 min-w-0 flex-1">{props.children}</div>
  </div>
);
