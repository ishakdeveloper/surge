import type { MapCar, MapFlash, MapView } from "@/components/map/surge-map.js";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { cityAtom, cityViewAtom, cityWatchAtom } from "@surge/common/atom/city-atoms";
import { Viewport } from "@surge/domain/realtime/Wire";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";

const NONE: ReadonlyArray<never> = [];

/**
 * The live city under a map — the web's `useCity` for the phone: the cars free
 * to take a trip in view, and a flash wherever one was just booked. Pass
 * `cars` and `flashes` to `SurgeMap`, and its `onView` this `onView` — the map
 * moving is what tells the gateway which part of the city to send.
 */
export const useCity = () => {
  useAtomMount(cityWatchAtom);
  const city = useAtomValue(cityAtom);
  const setView = useAtomSet(cityViewAtom);
  const latest = AsyncResult.isSuccess(city) ? city.value : undefined;

  const cars = React.useMemo(
    (): ReadonlyArray<MapCar> =>
      latest === undefined ? NONE : latest.cars.map((car) => ({
        key: car.key,
        position: { lat: car.lat, lng: car.lng },
        heading: car.heading,
      })),
    [latest],
  );

  const flashes = React.useMemo(
    (): ReadonlyArray<MapFlash> =>
      latest === undefined ? NONE : latest.flashes.map((flash) => ({
        key: flash.key,
        position: { lat: flash.lat, lng: flash.lng },
        seenAtMs: flash.seenAtMs,
      })),
    [latest],
  );

  const onView = React.useCallback(
    (view: MapView) => {
      setView({ viewport: Option.some(new Viewport(view)) });
    },
    [setView],
  );

  return { cars, flashes, onView };
};
