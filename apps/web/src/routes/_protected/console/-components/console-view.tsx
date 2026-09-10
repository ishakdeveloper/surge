import { fleetAtom, fleetWatchAtom, viewportAtom } from "@/atom/console-atoms.js";
import { SplitView } from "@/components/app/split-view.js";
import {
  type MapCell,
  type MapMarker,
  type MapView,
  SurgeMap,
} from "@/components/map/surge-map.js";
import { FleetStats } from "@/routes/_protected/console/-components/fleet-stats.js";
import { ShardTable } from "@/routes/_protected/console/-components/shard-table.js";
import { SimControls } from "@/routes/_protected/console/-components/sim-controls.js";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { Viewport } from "@surge/domain/realtime/Wire";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";

const NONE: ReadonlyArray<never> = [];

/**
 * The map is the subscription: panning tells the gateway what to send. Zoomed
 * out it sends counts per cell, shaded by how full each one is; zoomed in past
 * 14 it sends the drivers inside the view — idle in teal, busy in amber.
 */
export const ConsoleView = () => {
  useAtomMount(fleetWatchAtom);
  const fleet = useAtomValue(fleetAtom);
  const setView = useAtomSet(viewportAtom);
  const update = AsyncResult.isSuccess(fleet) ? fleet.value.latest : undefined;

  const cells = React.useMemo((): ReadonlyArray<MapCell> => {
    if (update === undefined) return NONE;
    const busiest = Math.max(1, ...update.cells.map((cell) => cell.drivers));
    return update.cells.map((cell) => ({
      id: cell.cell,
      boundary: cell.boundary,
      weight: cell.drivers / busiest,
    }));
  }, [update]);

  const dots = React.useMemo(
    (): ReadonlyArray<MapMarker> =>
      update === undefined ? NONE : update.drivers.map((driver) => ({
        id: driver.id,
        position: { lat: driver.lat, lng: driver.lng },
        kind: driver.status === "idle" && !driver.reserved ? "idle" : "driver",
      })),
    [update],
  );

  const onView = React.useCallback(
    (view: MapView) => {
      setView({ viewport: Option.some(new Viewport(view)) });
    },
    [setView],
  );

  return (
    <SplitView
      aside={
        <>
          <header className="flex flex-col gap-1">
            <h1 className="text-lg font-semibold">Console</h1>
            <p className="text-muted-foreground text-sm">
              {update === undefined
                ? "Waiting for the first frame…"
                : update.mode === "cells"
                ? "Drivers per cell. Zoom in to see them one by one."
                : `${update.drivers.length.toLocaleString()} drivers in view.`}
            </p>
          </header>
          <FleetStats />
          <SimControls />
          <ShardTable />
        </>
      }
    >
      <SurgeMap
        markers={NONE}
        dots={dots}
        cells={cells}
        route={NONE}
        follow={NONE}
        onView={onView}
      />
    </SplitView>
  );
};
