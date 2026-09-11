import type { MapPoint } from "@/components/map/surge-map.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import type { Place } from "@surge/client/Places";
import {
  placeAtAtom,
  type PlaceField as Field,
  placeQueryAtom,
  placeResultsAtom,
  PointKey,
} from "@surge/common/atom/place-atoms";
import { formatPoint } from "@surge/common/lib/format";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { ArrowUpDown, MapPin, X } from "lucide-react";
import * as React from "react";

/**
 * A stop: exactly where the car should go, and the place it is when the rider
 * chose one by name. A point tapped on the map has no name yet; `StopName`
 * looks one up.
 */
export interface Stop {
  readonly position: MapPoint;
  readonly place: Option.Option<Place>;
}

/** A stop's name as a rider reads it. */
export const StopName = (props: { readonly stop: Stop; }) =>
  Option.match(props.stop.place, {
    onSome: (place) => <>{place.name}</>,
    onNone: () => <PointName point={props.stop.position} />,
  });

/**
 * The address nearest a point. When search is down the point is still exactly
 * where the rider put it, so it is shown as a position rather than hidden.
 */
const PointName = (props: { readonly point: MapPoint; }) => {
  const place = useAtomValue(placeAtAtom(PointKey.make(props.point)));

  if (AsyncResult.isSuccess(place)) {
    return Option.match(place.value, {
      onNone: () => <>Pin on the map</>,
      onSome: (found) => <>{found.name}</>,
    });
  }
  if (AsyncResult.isFailure(place)) return <>{formatPoint(props.point)}</>;
  return <>Finding the address…</>;
};

/**
 * Where a stop sits on the rail: the pickup a yellow disc ringed in ink, the
 * dropoff an ink square with a yellow heart — the same marks the map stands
 * at the two ends of the route.
 */
export const StopDot = (props: { readonly kind: Field; }) =>
  props.kind === "pickup"
    ? (
      <span
        aria-hidden
        className="grid size-4 place-items-center rounded-full bg-primary ring-[3px] ring-foreground ring-inset"
      >
        <span className="size-1.5 rounded-full bg-foreground" />
      </span>
    )
    : (
      <span aria-hidden className="grid size-4 place-items-center rounded-[5px] bg-foreground">
        <span className="size-1.5 rounded-[2px] bg-primary" />
      </span>
    );

/** A stop on the rail: its mark, what it is, and where. */
const StopLine = (props: {
  readonly kind: Field;
  readonly label: string;
  readonly children: React.ReactNode;
}) => (
  <>
    <span className="grid w-5 shrink-0 place-items-center">
      <StopDot kind={props.kind} />
    </span>
    <span className="flex min-w-0 flex-1 flex-col">
      <span className="text-xs font-semibold text-muted-foreground">{props.label}</span>
      <span className="truncate text-base font-semibold">{props.children}</span>
    </span>
  </>
);

/** A stop that is read, not edited: the trip card's. */
export const StopRow = (props: {
  readonly kind: Field;
  readonly label: string;
  readonly stop: Stop;
}) => (
  <div className="flex items-center gap-3 px-3 py-2.5">
    <StopLine kind={props.kind} label={props.label}>
      <StopName stop={props.stop} />
    </StopLine>
  </div>
);

/**
 * The trip's two stops on one grey card, joined by a dotted rail the way a
 * route map joins its stations. The rail fades while a stop is being edited,
 * because the places found open between the two.
 */
export const StopsCard = (props: {
  readonly rail: boolean;
  readonly footer: React.ReactNode;
  readonly children: React.ReactNode;
}) => (
  <div className="rounded-2xl bg-tile p-1.5">
    <div className="relative flex flex-col gap-0.5">
      <span
        aria-hidden
        className={cn(
          "pointer-events-none absolute top-[40px] bottom-[40px] left-[21px] border-l-2 border-dotted border-foreground/25 transition-opacity duration-150",
          !props.rail && "opacity-0",
        )}
      />
      {props.children}
    </div>
    {props.footer}
  </div>
);

/** Turns the trip around: the arrows turn with it. */
export const SwapStops = (props: { readonly onSwap: () => void; }) => {
  const [turns, setTurns] = React.useState(0);

  return (
    <button
      type="button"
      aria-label="Swap pickup and dropoff"
      onClick={() => {
        setTurns((count) => count + 1);
        props.onSwap();
      }}
      className="absolute top-1/2 right-2 grid size-9 -translate-y-1/2 cursor-pointer place-items-center rounded-full bg-card shadow-[0_1px_2px_rgb(0_0_0/0.08)] transition-colors hover:bg-accent motion-safe:animate-pop-in"
    >
      <ArrowUpDown
        aria-hidden
        className="size-4 transition-transform duration-300 ease-out"
        style={{ transform: `rotate(${turns * 180}deg)` }}
      />
    </button>
  );
};

/** Fewer characters than this and the field waits for more instead of searching. */
const MIN_QUERY = 3;

/**
 * From or To, edited in place: pressing the stop lifts its row into a white
 * field in the same position, the places found rise in beneath it, and
 * choosing one settles it back into the card.
 *
 * A combobox in the ARIA sense: focus stays in the input, the arrow keys move
 * through the places found, Enter takes one and Escape puts the stop back.
 * Which stop is being edited belongs to the sheet, so the rest of it can step
 * aside while the rider searches.
 */
export const PlaceField = (props: {
  readonly field: Field;
  readonly label: string;
  readonly placeholder: string;
  readonly stop: Option.Option<Stop>;
  readonly editing: boolean;
  readonly onEdit: () => void;
  readonly onDone: () => void;
  readonly onChoose: (stop: Stop) => void;
}) => {
  const [active, setActive] = React.useState(0);
  const query = useAtomValue(placeQueryAtom(props.field));
  const setQuery = useAtomSet(placeQueryAtom(props.field));
  const results = useAtomValue(placeResultsAtom(props.field));
  const input = React.useRef<HTMLInputElement>(null);
  const button = React.useRef<HTMLButtonElement>(null);
  // Set when the field closes by the keyboard or a choice, which should hand
  // focus back to the stop; a close by tabbing away should not take it back.
  const refocus = React.useRef(false);
  const id = React.useId();
  const listId = `${id}-places`;

  React.useEffect(() => {
    if (props.editing) {
      input.current?.focus();
    } else if (refocus.current) {
      refocus.current = false;
      button.current?.focus();
    }
  }, [props.editing]);

  const close = (andRefocus: boolean) => {
    refocus.current = andRefocus;
    setQuery("");
    setActive(0);
    props.onDone();
  };

  const choose = (place: Place) => {
    props.onChoose({ position: place.position, place: Option.some(place) });
    close(true);
  };

  if (!props.editing) {
    return (
      <button
        ref={button}
        type="button"
        onClick={() => {
          setQuery("");
          setActive(0);
          props.onEdit();
        }}
        className="flex w-full cursor-pointer items-center gap-3 rounded-xl py-2.5 pr-12 pl-3 text-left transition-colors duration-150 hover:bg-tile-hover"
      >
        <StopLine kind={props.field} label={props.label}>
          {Option.match(props.stop, {
            onNone: () => (
              <span className="font-medium text-muted-foreground">{props.placeholder}</span>
            ),
            onSome: (stop) => <StopName stop={stop} />,
          })}
        </StopLine>
      </button>
    );
  }

  const typed = query.trim();
  // The places on offer right now, for the keyboard. Rendering below reads the
  // result itself, so loading and failure keep their own states.
  const offered = typed.length >= MIN_QUERY && AsyncResult.isSuccess(results)
      && Option.isSome(results.value)
    ? results.value.value
    : undefined;
  const showing = offered !== undefined && offered.length > 0;

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      close(true);
      return;
    }
    if (!showing) return;
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive((index) => Math.min(index + 1, offered.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive((index) => Math.max(index - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      const place = offered[Math.min(active, offered.length - 1)];
      if (place !== undefined) choose(place);
    }
  };

  return (
    <div className="rounded-xl bg-card shadow-[0_1px_2px_rgb(0_0_0/0.06),0_10px_24px_-12px_rgb(0_0_0/0.22)]">
      <div className="flex items-center gap-3 py-2.5 pr-2.5 pl-3">
        <span className="grid w-5 shrink-0 place-items-center">
          <StopDot kind={props.field} />
        </span>
        <label className="flex min-w-0 flex-1 flex-col">
          <span className="text-xs font-semibold text-muted-foreground">{props.label}</span>
          <input
            ref={input}
            role="combobox"
            aria-expanded={showing}
            aria-controls={listId}
            aria-autocomplete="list"
            aria-activedescendant={showing ? `${listId}-${active}` : undefined}
            autoComplete="off"
            spellCheck={false}
            enterKeyHint="search"
            value={query}
            placeholder={Option.isSome(props.stop)
              ? "Search for another place"
              : "Street, place or postcode"}
            onChange={(event) => {
              setQuery(event.target.value);
              setActive(0);
            }}
            onBlur={() => {
              close(false);
            }}
            onKeyDown={onKeyDown}
            className="w-full bg-transparent p-0 text-base font-semibold outline-none placeholder:font-medium placeholder:text-muted-foreground"
          />
        </label>
        {query !== "" && (
          <button
            type="button"
            aria-label={`Clear ${props.label}`}
            // Keeps focus in the input, which a press would otherwise take.
            onMouseDown={(event) => {
              event.preventDefault();
            }}
            onClick={() => {
              setQuery("");
              setActive(0);
            }}
            className="grid size-7 shrink-0 cursor-pointer place-items-center rounded-full bg-tile text-muted-foreground transition-colors hover:bg-tile-hover hover:text-foreground motion-safe:animate-pop-in"
          >
            <X aria-hidden className="size-3.5" strokeWidth={2.5} />
          </button>
        )}
      </div>

      <SearchState typed={typed} results={results} />
      <ul
        id={listId}
        role="listbox"
        aria-label={`Places for ${props.label}`}
        className={cn("flex flex-col px-1.5", showing && "border-t border-border py-1.5")}
      >
        {showing && offered.map((place, index) => (
          // Focus stays in the input, which owns the keys; a pointer picks
          // an option without taking focus from it.
          // oxlint-disable-next-line jsx-a11y/click-events-have-key-events
          <li
            key={`${place.name}|${place.area}`}
            id={`${listId}-${index}`}
            role="option"
            aria-selected={index === active}
            onMouseDown={(event) => {
              event.preventDefault();
            }}
            onMouseEnter={() => {
              setActive(index);
            }}
            onClick={() => {
              choose(place);
            }}
            style={{ animationDelay: `${index * 25}ms` }}
            className={cn(
              "flex cursor-pointer items-center gap-3 rounded-lg px-2 py-2 transition-colors duration-100 motion-safe:animate-rise-in",
              index === active && "bg-tile",
            )}
          >
            <span
              aria-hidden
              className={cn(
                "grid size-8 shrink-0 place-items-center rounded-full text-muted-foreground transition-colors duration-100",
                index === active ? "bg-card" : "bg-tile",
              )}
            >
              <MapPin className="size-4" />
            </span>
            <span className="flex min-w-0 flex-col">
              <span className="truncate text-[15px] font-semibold">{place.name}</span>
              {place.area !== "" && (
                <span className="truncate text-[13px] text-muted-foreground">{place.area}</span>
              )}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
};

/**
 * What the search is doing when it is not showing places: searching, not
 * answering, or finding nothing. Too little typed says nothing at all — the
 * placeholder already asks for more.
 */
const SearchState = (props: {
  readonly typed: string;
  readonly results: AsyncResult.AsyncResult<Option.Option<ReadonlyArray<Place>>, unknown>;
}) => {
  if (props.typed.length < MIN_QUERY) return null;

  const line = (text: string) => (
    <p role="status" className="border-t border-border px-4 py-3 text-sm text-muted-foreground">
      {text}
    </p>
  );

  if (AsyncResult.isFailure(props.results)) {
    return line("Address search is not answering. Tap the map, or pick a popular trip.");
  }
  if (!AsyncResult.isSuccess(props.results) || Option.isNone(props.results.value)) {
    return line("Searching…");
  }
  if (props.results.value.value.length === 0) {
    return line(`Nothing in Amsterdam matches “${props.typed}”.`);
  }
  return null;
};
