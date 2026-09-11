import { Geolocation } from "@surge/client/Geolocation";
import { insideAmsterdam, Places } from "@surge/client/Places";
import type { LatLng } from "@surge/domain/geo/Polyline";
import { Data, Effect, Option } from "effect";
import { Atom } from "effect/unstable/reactivity";
import { runtime } from "./runtime.js";

/**
 * Finding places, for both apps: what a rider types into From and To, the
 * results it finds, and the name of a point chosen on the map.
 */

/** Which of the two place fields a search belongs to. */
export type PlaceField = "pickup" | "dropoff";

/** What the rider has typed into a field, kept per field so the two never share. */
export const placeQueryAtom = Atom.family((_field: PlaceField) => Atom.make(""));

/**
 * The query once typing pauses. A search per keystroke would ask Photon six
 * times for "Rijks" and show five answers nobody waited for.
 */
const settledQueryAtom = Atom.family((field: PlaceField) =>
  placeQueryAtom(field).pipe(Atom.debounce("250 millis"))
);

/** Shorter than this is too little to search on: "Da" matches half the city. */
const MIN_QUERY = 3;

/**
 * Places for a field's query. `none` while there is too little to search on,
 * which is a different state from a search that found nothing.
 */
export const placeResultsAtom = Atom.family((field: PlaceField) =>
  runtime.atom(Effect.fnUntraced(function*(get) {
    const query = get(settledQueryAtom(field)).trim();
    if (query.length < MIN_QUERY) return Option.none();
    const places = yield* Places;
    return Option.some(yield* places.search(query));
  }))
);

/**
 * A point as a family key, rounded to about a metre so the same tap asked
 * twice is one lookup rather than two.
 */
export class PointKey extends Data.Class<{ readonly lat: number; readonly lng: number; }> {
  static make(point: LatLng) {
    return new PointKey({
      lat: Math.round(point.lat * 1e5) / 1e5,
      lng: Math.round(point.lng * 1e5) / 1e5,
    });
  }
}

/** The place at a point, for a pickup or dropoff chosen on the map. */
export const placeAtAtom = Atom.family((key: PointKey) =>
  runtime.atom(Effect.gen(function*() {
    const places = yield* Places;
    return yield* places.at(key);
  }))
);

/**
 * Where the rider is, when that is somewhere Surge can pick them up: the
 * pickup a booking starts from. `none` outside Amsterdam, where a pickup
 * would be refused anyway.
 */
export const nearbyPickupAtom = runtime.atom(Effect.gen(function*() {
  const geolocation = yield* Geolocation;
  const here = yield* geolocation.current;
  return insideAmsterdam(here) ? Option.some(here) : Option.none();
}));
