import { cn } from "@/lib/utils.js";
import type { MapboxOverlay } from "@deck.gl/mapbox";
import type { Map as MapLibreMap } from "maplibre-gl";
import * as React from "react";

/**
 * The map every surface draws on: MapLibre for the streets, deck.gl on top for
 * everything that moves.
 *
 * Two libraries because they are good at different things. MapLibre renders a
 * vector basemap well and nothing else cheaply; deck.gl draws tens of thousands
 * of points in one WebGL call, where the same number of DOM markers — or React
 * components — would freeze the tab. The console needs the second, so every
 * surface uses the same pairing rather than two map stacks.
 *
 * Both are loaded on mount, not imported at the top of the file. They need
 * WebGL and a real `window`; the page tests render every page under jsdom,
 * which has neither, and a module that touched WebGL on import would fail those
 * tests for a reason unrelated to the page. So the check comes first and the
 * import second, and without WebGL the component says so instead of crashing.
 */

export interface MapPoint {
  readonly lat: number;
  readonly lng: number;
}

export interface MapMarker {
  readonly id: string;
  readonly position: MapPoint;
  /** Categorisation, which is the only thing colour is used for here. */
  readonly kind: "pickup" | "dropoff" | "driver" | "idle" | "self";
}

/** A shaded region — for the console, one H3 cell and how full it is. */
export interface MapCell {
  readonly id: string;
  /** The outline as `[lng, lat]` pairs, in order. */
  readonly boundary: ReadonlyArray<readonly [number, number]>;
  /** 0 to 1: how strongly to shade it. */
  readonly weight: number;
}

/** What the map is showing: its bounds and zoom. */
export interface MapView {
  readonly west: number;
  readonly south: number;
  readonly east: number;
  readonly north: number;
  readonly zoom: number;
}

export interface SurgeMapProps {
  readonly markers: ReadonlyArray<MapMarker>;
  /**
   * Many small points — the console's fleet. Drawn under `markers`, smaller and
   * unoutlined, because thousands of outlined discs become one grey smear.
   */
  readonly dots?: ReadonlyArray<MapMarker>;
  /** Shaded regions, drawn beneath everything else. */
  readonly cells?: ReadonlyArray<MapCell>;
  readonly route: ReadonlyArray<MapPoint>;
  /**
   * The view is fitted to these whenever the set changes — not on every render,
   * which would snatch the map back each time a driver moved while somebody was
   * trying to pan it.
   */
  readonly follow: ReadonlyArray<MapPoint>;
  /** A click on the map, for pages where the map is how a point is chosen. */
  readonly onPick?: (point: MapPoint) => void;
  /**
   * The view, once the map has loaded and again whenever a pan or zoom comes
   * to rest — not during one, which would be sixty calls a second.
   */
  readonly onView?: (view: MapView) => void;
  readonly className?: string;
}

/** OpenFreeMap: vector tiles with no API key, which is what a demo can depend on. */
const STYLE = "https://tiles.openfreemap.org/styles/dark";

/** Amsterdam Centraal, roughly — where a map with nothing to show starts. */
const CENTRE: [number, number] = [4.9003, 52.3731];

const COLOURS: Record<MapMarker["kind"], [number, number, number]> = {
  pickup: [34, 197, 94],
  dropoff: [96, 165, 250],
  driver: [245, 158, 11],
  idle: [45, 212, 191],
  self: [240, 240, 240],
};

const NONE: ReadonlyArray<never> = [];

type Layers = typeof import("@deck.gl/layers");

type Status = "loading" | "ready" | "unsupported";

export const SurgeMap = (props: SurgeMapProps) => {
  const container = React.useRef<HTMLDivElement>(null);
  const map = React.useRef<MapLibreMap | null>(null);
  const overlay = React.useRef<MapboxOverlay | null>(null);
  const layers = React.useRef<Layers | null>(null);
  const onPick = React.useRef(props.onPick);
  const onView = React.useRef(props.onView);
  const [status, setStatus] = React.useState<Status>("loading");

  onPick.current = props.onPick;
  onView.current = props.onView;

  React.useEffect(() => {
    // Checked by constructor rather than by asking a canvas for a context:
    // jsdom has no WebGL2RenderingContext at all, and asking its canvas for one
    // prints a "not implemented" error into every test run.
    if (typeof WebGL2RenderingContext === "undefined" || container.current === null) {
      setStatus("unsupported");
      return;
    }

    let cancelled = false;
    const element = container.current;

    void Promise.all([
      import("maplibre-gl"),
      import("@deck.gl/mapbox"),
      import("@deck.gl/layers"),
      import("maplibre-gl/dist/maplibre-gl.css"),
    ]).then(([maplibre, deck, deckLayers]) => {
      if (cancelled) return;

      const instance = new maplibre.Map({
        container: element,
        style: STYLE,
        center: CENTRE,
        zoom: 12.5,
        attributionControl: { compact: true },
      });
      const deckOverlay = new deck.MapboxOverlay({ interleaved: false, layers: [] });
      instance.addControl(deckOverlay);
      instance.on("click", (event) => {
        onPick.current?.({ lat: event.lngLat.lat, lng: event.lngLat.lng });
      });
      const reportView = () => {
        const bounds = instance.getBounds();
        onView.current?.({
          west: bounds.getWest(),
          south: bounds.getSouth(),
          east: bounds.getEast(),
          north: bounds.getNorth(),
          zoom: instance.getZoom(),
        });
      };
      instance.on("load", reportView);
      instance.on("moveend", reportView);

      map.current = instance;
      overlay.current = deckOverlay;
      layers.current = deckLayers;
      setStatus("ready");
    });

    return () => {
      cancelled = true;
      overlay.current?.finalize();
      map.current?.remove();
      map.current = null;
      overlay.current = null;
    };
  }, []);

  React.useEffect(() => {
    const deckLayers = layers.current;
    if (status !== "ready" || overlay.current === null || deckLayers === null) return;

    overlay.current.setProps({
      layers: [
        new deckLayers.PolygonLayer<MapCell>({
          id: "cells",
          data: props.cells ?? NONE,
          getPolygon: (cell) => cell.boundary.map(([lng, lat]): [number, number] => [lng, lat]),
          getFillColor: (cell) => [245, 158, 11, Math.round(20 + 170 * cell.weight)],
          getLineColor: [245, 158, 11, 90],
          lineWidthMinPixels: 1,
          stroked: true,
          filled: true,
          updateTriggers: { getFillColor: props.cells },
        }),
        new deckLayers.PathLayer<ReadonlyArray<MapPoint>>({
          id: "route",
          data: props.route.length > 1 ? [props.route] : [],
          getPath: (path) => path.map((point): [number, number] => [point.lng, point.lat]),
          getColor: [96, 165, 250, 200],
          widthMinPixels: 4,
        }),
        new deckLayers.ScatterplotLayer<MapMarker>({
          id: "dots",
          data: props.dots ?? NONE,
          getPosition: (marker) => [marker.position.lng, marker.position.lat],
          getFillColor: (marker) => COLOURS[marker.kind],
          radiusMinPixels: 2.5,
          radiusMaxPixels: 6,
          getRadius: 8,
          updateTriggers: { getFillColor: props.dots },
        }),
        new deckLayers.ScatterplotLayer<MapMarker>({
          id: "markers",
          data: props.markers,
          getPosition: (marker) => [marker.position.lng, marker.position.lat],
          getFillColor: (marker) => COLOURS[marker.kind],
          getLineColor: [20, 20, 24],
          stroked: true,
          lineWidthMinPixels: 2,
          radiusMinPixels: 7,
          getRadius: 12,
        }),
      ],
    });
  }, [status, props.markers, props.route, props.dots, props.cells]);

  // A string key, so the effect fires when the points change rather than when
  // the array identity does — a parent re-rendering with equal points must not
  // move the camera.
  const followKey = props.follow.map((point) => `${point.lat.toFixed(5)},${point.lng.toFixed(5)}`)
    .join("|");

  React.useEffect(() => {
    const instance = map.current;
    if (status !== "ready" || instance === null || props.follow.length === 0) return;

    if (props.follow.length === 1) {
      const [only] = props.follow;
      instance.easeTo({ center: [only!.lng, only!.lat], zoom: Math.max(instance.getZoom(), 14) });
      return;
    }

    const lngs = props.follow.map((point) => point.lng);
    const lats = props.follow.map((point) => point.lat);
    instance.fitBounds(
      [[Math.min(...lngs), Math.min(...lats)], [Math.max(...lngs), Math.max(...lats)]],
      { padding: 64, maxZoom: 16, duration: 600 },
    );
    // `followKey` stands in for `props.follow`, deliberately.
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [status, followKey]);

  return (
    <div
      className={cn(
        "relative min-h-64 flex-1 overflow-hidden rounded-md border border-border",
        props.className,
      )}
    >
      <div ref={container} className="absolute inset-0" />
      {status !== "ready" && (
        <p className="text-muted-foreground absolute inset-0 grid place-items-center p-4 text-center text-sm">
          {status === "unsupported"
            ? "The map needs WebGL, which this browser does not provide."
            : "Loading the map…"}
        </p>
      )}
    </div>
  );
};
