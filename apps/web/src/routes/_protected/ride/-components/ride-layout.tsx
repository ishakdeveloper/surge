import type { MapInset } from "@/components/map/surge-map.js";
import { SignStack } from "@/components/sign/sign.js";
import * as React from "react";

/** Where the sheet stops sitting under the map and starts floating over it. */
const WIDE = "(min-width: 1024px)";

/** The sheet's width at desktop size, plus its offset from the map's edge. */
const SHEET_EDGE = 420 + 16 + 40;

/**
 * Where the map is covered, so a fitted route lands where it can be seen. On a
 * phone the sheet overlaps the bottom of the map's band by its rounded top;
 * wider, it floats over the map's left side.
 */
export const useRideMapInset = (): MapInset => {
  const [wide, setWide] = React.useState(() => globalThis.matchMedia(WIDE).matches);

  React.useEffect(() => {
    const query = globalThis.matchMedia(WIDE);
    const change = () => {
      setWide(query.matches);
    };
    query.addEventListener("change", change);
    return () => {
      query.removeEventListener("change", change);
    };
  }, []);

  // The floating navigation covers the map's top 70px or so, on every width.
  return wide
    ? { top: 96, right: 64, bottom: 64, left: SHEET_EDGE }
    : { top: 84, right: 40, bottom: 64, left: 40 };
};

/**
 * The rider's page, designed for a phone first: the map in a band across the
 * top, and a white sheet rising over its lower edge. The sheet's cards scroll;
 * its footer — the one action that moves the trip on — does not, so it is
 * always under the thumb. From desktop width the map takes the whole page and
 * the same sheet floats over its left side, footer and all, so the route and
 * the car are never under a card.
 */
export const RideLayout = (props: {
  readonly title: string;
  readonly map: React.ReactNode;
  /** The pinned action, or `null` when the sheet has none right now. */
  readonly footer: React.ReactNode;
  readonly children: React.ReactNode;
}) => (
  <div className="flex min-h-0 flex-1 flex-col lg:relative">
    <h1 className="sr-only">{props.title}</h1>
    {
      /* Below `lg` the sheet overlaps the map by 24px, so the map's credit —
        which OpenStreetMap's licence requires be readable — lifts clear of it. */
    }
    <div className="relative flex h-[42dvh] shrink-0 max-lg:[&_.maplibregl-ctrl-bottom-right]:bottom-9 lg:absolute lg:inset-0 lg:h-auto">
      {props.map}
    </div>
    {
      /* Wider, this spans the page's height over the map, so it lets clicks
        through everywhere the sheet itself does not cover. */
    }
    <div className="relative z-10 -mt-6 flex min-h-0 flex-1 flex-col rounded-t-3xl bg-card shadow-[0_-10px_30px_-18px_rgb(0_0_0/0.35)] lg:pointer-events-none lg:absolute lg:top-[76px] lg:bottom-0 lg:left-0 lg:mt-0 lg:w-[452px] lg:rounded-none lg:bg-transparent lg:p-4 lg:pt-0 lg:shadow-none">
      <div className="flex min-h-0 flex-1 flex-col lg:pointer-events-auto lg:max-h-full lg:flex-none lg:rounded-3xl lg:bg-card lg:shadow-[0_18px_44px_-18px_rgb(0_0_0/0.35)]">
        <div className="min-h-0 flex-1 overflow-y-auto px-3 pt-2.5 pb-3 lg:p-3">
          <span
            aria-hidden
            className="mx-auto mb-2.5 block h-1 w-10 rounded-full bg-foreground/15 lg:hidden"
          />
          <SignStack>{props.children}</SignStack>
        </div>
        {props.footer !== null && (
          <div className="shrink-0 border-t border-border/70 px-3 pt-2.5 pb-[max(env(safe-area-inset-bottom),0.75rem)] lg:border-0 lg:px-3 lg:pt-0 lg:pb-3">
            {props.footer}
          </div>
        )}
      </div>
    </div>
  </div>
);
