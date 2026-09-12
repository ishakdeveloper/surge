import { cn } from "@/lib/utils.js";
import type { MapboxOverlay } from "@deck.gl/mapbox";
import type { Map as MapboxMap } from "mapbox-gl/esm";
import * as React from "react";

/**
 * The map every surface draws on: Mapbox for the streets, deck.gl on top for
 * everything that moves.
 *
 * Two libraries because they are good at different things. Mapbox renders a
 * vector basemap well and nothing else cheaply; deck.gl draws tens of thousands
 * of points in one WebGL call, where the same number of DOM markers — or React
 * components — would freeze the tab. The console needs the second, so every
 * surface uses the same pairing rather than two map stacks — and `MapboxOverlay`
 * is deck.gl's own answer for exactly this pairing.
 *
 * Mapbox rather than a keyless basemap because the phone draws Mapbox too, and
 * one city should not look like two products. It wants a token; without one the
 * map says so rather than showing an empty grey box.
 *
 * The city's own life is drawn the same way: the free cars gliding between the
 * positions the gateway reports once a second, and a flash where a trip was
 * just booked. Those two are animated frame by frame, and only while something
 * is moving — a map at rest draws nothing, so a phone showing one is not
 * spending its battery on a still picture.
 *
 * Both libraries are loaded on mount, not imported at the top of the file.
 * They need WebGL and a real `window`; the page tests render every page under
 * jsdom, which has neither, and a module that touched WebGL on import would
 * fail those tests for a reason unrelated to the page. So the check comes first
 * and the import second, and without WebGL the component says so instead of
 * crashing.
 */

export interface MapPoint {
  readonly lat: number;
  readonly lng: number;
}

export interface MapMarker {
  readonly id: string;
  readonly position: MapPoint;
  /**
   * What the point is. A trip's stops and its driver stand as signs — the same
   * marks as the pictograms beside their names — and everything else as a dot.
   */
  readonly kind: "pickup" | "dropoff" | "driver" | "idle" | "self";
}

/** A car free to take a trip, from the city feed. */
export interface MapCar {
  /** The same car across updates, which is what lets it glide rather than jump. */
  readonly key: string;
  readonly position: MapPoint;
  /** Degrees clockwise from north. */
  readonly heading: number;
}

/** A trip just booked: the middle of the area it was booked in, and when this map first heard. */
export interface MapFlash {
  readonly key: string;
  readonly position: MapPoint;
  /** Wall-clock milliseconds, which is when the flash starts. */
  readonly seenAtMs: number;
}

/** A shaded region — for the console, one H3 cell and how full it is. */
export interface MapCell {
  readonly id: string;
  /** The outline as `[lng, lat]` pairs, in order. */
  readonly boundary: ReadonlyArray<readonly [number, number]>;
  /** 0 to 1: how strongly to shade it. */
  readonly weight: number;
  /**
   * 0 to 1: how hard the cell is surging. A surging cell is drawn red instead
   * of amber, so price reads apart from how many cars are there.
   */
  readonly heat?: number;
}

/** What the map is showing: its bounds and zoom. */
export interface MapView {
  readonly west: number;
  readonly south: number;
  readonly east: number;
  readonly north: number;
  readonly zoom: number;
}

/** Room kept clear at each edge when the view is fitted, in pixels. */
export interface MapInset {
  readonly top: number;
  readonly right: number;
  readonly bottom: number;
  readonly left: number;
}

export interface SurgeMapProps {
  readonly markers: ReadonlyArray<MapMarker>;
  /**
   * Many small points — the console's fleet. Drawn under `markers`, smaller and
   * unoutlined, because thousands of outlined discs become one grey smear.
   */
  readonly dots?: ReadonlyArray<MapMarker>;
  /** The free cars around, drawn under the trip so the rider's own route reads first. */
  readonly cars?: ReadonlyArray<MapCar>;
  /** Trips just booked, each flashing once where it was booked. */
  readonly flashes?: ReadonlyArray<MapFlash>;
  /** Shaded regions, drawn beneath everything else. */
  readonly cells?: ReadonlyArray<MapCell>;
  readonly route: ReadonlyArray<MapPoint>;
  /**
   * The view is fitted to these whenever the set changes — not on every render,
   * which would snatch the map back each time a driver moved while somebody was
   * trying to pan it.
   */
  readonly follow: ReadonlyArray<MapPoint>;
  /**
   * Where the page covers the map — the rider's sign stack — so a fitted route
   * lands in the part that can be seen.
   */
  readonly inset?: MapInset;
  /** A click on the map, for pages where the map is how a point is chosen. */
  readonly onPick?: (point: MapPoint) => void;
  /**
   * The view, once the map has loaded and again whenever a pan or zoom comes
   * to rest — not during one, which would be sixty calls a second.
   */
  readonly onView?: (view: MapView) => void;
  readonly className?: string;
}

/**
 * Mapbox's Light: near-white ground and pale grey streets, so the route's sign
 * yellow is the loudest thing on the map. The phone asks for the same style by
 * the same name.
 */
const STYLE = "mapbox://styles/mapbox/light-v11";

/**
 * The public token, which ships in the client bundle and signs every tile
 * request — public by design, and scoped and URL-restricted in Mapbox rather
 * than kept secret. Read straight from `import.meta.env` for the same reason
 * the Stripe key is: what needs it is a map constructor, not an Effect.
 */
const TOKEN = import.meta.env.VITE_MAPBOX_TOKEN;

/** Amsterdam Centraal, roughly — where a map with nothing to show starts. */
const CENTRE: [number, number] = [4.9003, 52.3731];

const INK: [number, number, number] = [26, 26, 26];
const YELLOW: [number, number, number] = [255, 210, 0];

const rgba = (rgb: ReadonlyArray<number>, alpha: number): [number, number, number, number] => [
  rgb[0] ?? 0,
  rgb[1] ?? 0,
  rgb[2] ?? 0,
  Math.round(Math.max(0, Math.min(255, alpha))),
];

/** Dots: the console's fleet and the driver's own position. */
const DOT_COLOURS: Record<MapMarker["kind"], [number, number, number]> = {
  pickup: YELLOW,
  dropoff: YELLOW,
  driver: INK,
  idle: [6, 122, 62],
  self: YELLOW,
};

/** A marker drawn as SVG, at twice its size on screen so it stays sharp. */
const signMarker = (body: string) =>
  `data:image/svg+xml;charset=utf-8,${
    encodeURIComponent(
      `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 32 32">${body}</svg>`,
    )
  }`;

/**
 * The same two marks the sheet's rail carries: the pickup a yellow disc ringed
 * in ink, the dropoff an ink square with a yellow heart, and the driver an ink
 * disc with a car — a car is a service, not a direction. Each sits on a white
 * halo so it holds against any street.
 */
const SIGN_MARKERS = {
  pickup: signMarker(
    `<circle cx="16" cy="16" r="14" fill="#ffffff"/><circle cx="16" cy="16" r="10.5" fill="#ffd200" stroke="#1a1a1a" stroke-width="3.5"/><circle cx="16" cy="16" r="3.5" fill="#1a1a1a"/>`,
  ),
  dropoff: signMarker(
    `<rect x="2.5" y="2.5" width="27" height="27" rx="9" fill="#ffffff"/><rect x="5" y="5" width="22" height="22" rx="7" fill="#1a1a1a"/><rect x="12" y="12" width="8" height="8" rx="2.5" fill="#ffd200"/>`,
  ),
  driver: signMarker(
    `<circle cx="16" cy="16" r="14" fill="#ffffff"/><circle cx="16" cy="16" r="12" fill="#1a1a1a"/><path d="M9.5 18.5v-3l1.9-3.9h9.2l1.9 3.9v3z" fill="none" stroke="#ffffff" stroke-width="1.8" stroke-linejoin="round"/><circle cx="12.6" cy="20.2" r="1.5" fill="#ffffff"/><circle cx="19.4" cy="20.2" r="1.5" fill="#ffffff"/>`,
  ),
} as const;

/**
 * A free car: an ink arrowhead on a white disc, pointing where it is heading.
 * A wayfinding arrow rather than a car drawn from above, so the city's cars
 * read as traffic moving and not as a fleet of little toys — and smaller and
 * plainer than the rider's own driver, which stays the one car with a sign.
 */
const CAR_ICON = signMarker(
  `<circle cx="16" cy="16" r="12" fill="#1a1a1a" fill-opacity="0.12"/><circle cx="16" cy="16" r="10.25" fill="#ffffff"/><path d="M16 9.5 21.2 21.4 16 18.7 10.8 21.4Z" fill="#1a1a1a" stroke="#1a1a1a" stroke-width="1" stroke-linejoin="round"/>`,
);

type SignKind = keyof typeof SIGN_MARKERS;

const isSign = (marker: MapMarker): marker is MapMarker & { readonly kind: SignKind; } =>
  marker.kind === "pickup" || marker.kind === "dropoff" || marker.kind === "driver";

const EDGE = 64;
const EVEN: MapInset = { top: EDGE, right: EDGE, bottom: EDGE, left: EDGE };

const NONE: ReadonlyArray<never> = [];

/** How long a car takes to reach its next reported position: the feed's own second. */
const GLIDE_MS = 1000;

/**
 * A booking's flash: a yellow dot that pops in and holds, with two rings
 * spreading from it, then fades. Long enough to be caught from the corner of
 * an eye, short enough that a busy evening is a scatter of sparks rather than
 * a map covered in dots.
 */
const FLASH_MS = 2400;
const FLASH_POP_MS = 180;
const FLASH_FADE_MS = 600;
const RING_MS = 1400;
const RING_DELAYS = [0, 480] as const;

type LngLat = [number, number];

/** A car's move from where it was drawn to where it was last reported. */
interface Glide {
  readonly from: LngLat;
  readonly to: LngLat;
  readonly heading: number;
  readonly startedAt: number;
}

interface CarAt {
  readonly position: LngLat;
  readonly heading: number;
}

interface FlashAt {
  readonly position: LngLat;
  readonly age: number;
}

interface RingAt {
  readonly position: LngLat;
  readonly progress: number;
}

const lngLat = (point: MapPoint): LngLat => [point.lng, point.lat];

const easeOut = (t: number) => 1 - (1 - t) ** 3;

const glideAt = (glide: Glide, now: number, still: boolean): LngLat => {
  if (still) return glide.to;
  // Constant speed, not eased: a car between two reports is still driving.
  const t = Math.min(1, Math.max(0, (now - glide.startedAt) / GLIDE_MS));
  return [
    glide.from[0] + (glide.to[0] - glide.from[0]) * t,
    glide.from[1] + (glide.to[1] - glide.from[1]) * t,
  ];
};

type Layers = typeof import("@deck.gl/layers");

type Status = "loading" | "ready" | "unsupported" | "unconfigured";

export const SurgeMap = (props: SurgeMapProps) => {
  const container = React.useRef<HTMLDivElement>(null);
  const map = React.useRef<MapboxMap | null>(null);
  const overlay = React.useRef<MapboxOverlay | null>(null);
  const layers = React.useRef<Layers | null>(null);
  const onPick = React.useRef(props.onPick);
  const onView = React.useRef(props.onView);
  const [status, setStatus] = React.useState<Status>("loading");
  // Counts the resizes that should refit the camera; see the `resize` listener.
  const [resizes, setResizes] = React.useState(0);
  // Whether the rider has moved the camera since the last fit.
  const touched = React.useRef(false);
  // Each car's current move, by key.
  const glides = React.useRef(new Map<string, Glide>());
  const frame = React.useRef<number | undefined>(undefined);
  const animateUntil = React.useRef(0);
  const reducedMotion = React.useRef(false);

  onPick.current = props.onPick;
  onView.current = props.onView;

  // What the static layers draw, derived once per change rather than once per
  // frame, so an animating map is not rebuilding its route sixty times a second.
  const path = React.useMemo(
    (): ReadonlyArray<ReadonlyArray<MapPoint>> => props.route.length > 1 ? [props.route] : NONE,
    [props.route],
  );
  const plain = React.useMemo(() => props.markers.filter((marker) => !isSign(marker)), [
    props.markers,
  ]);
  const signs = React.useMemo(() => props.markers.filter(isSign), [props.markers]);
  const scene = React.useRef({ props, path, plain, signs });
  scene.current = { props, path, plain, signs };

  React.useEffect(() => {
    const query = globalThis.matchMedia("(prefers-reduced-motion: reduce)");
    const change = () => {
      reducedMotion.current = query.matches;
    };
    change();
    query.addEventListener("change", change);
    return () => {
      query.removeEventListener("change", change);
    };
  }, []);

  React.useEffect(() => {
    // Checked by constructor rather than by asking a canvas for a context:
    // jsdom has no WebGL2RenderingContext at all, and asking its canvas for one
    // prints a "not implemented" error into every test run.
    if (typeof WebGL2RenderingContext === "undefined" || container.current === null) {
      setStatus("unsupported");
      return;
    }
    if (TOKEN === undefined || TOKEN === "") {
      setStatus("unconfigured");
      return;
    }

    let cancelled = false;
    const element = container.current;

    void Promise.all([
      // `mapbox-gl/esm`, not `mapbox-gl`: the package's default entry is UMD
      // despite the package saying `type: module`, and its worker lives inside
      // a form no bundler can follow. The ESM entry is real modules with named
      // exports and asks for its worker as a URL relative to itself, which is
      // the one shape Vite resolves — see the note beside `optimizeDeps`.
      import("mapbox-gl/esm"),
      import("@deck.gl/mapbox"),
      import("@deck.gl/layers"),
      import("mapbox-gl/dist/mapbox-gl.css"),
    ]).then(([mapbox, deck, deckLayers]) => {
      if (cancelled) return;

      const instance = new mapbox.Map({
        container: element,
        accessToken: TOKEN,
        style: STYLE,
        center: CENTRE,
        zoom: 12.5,
        // Mapbox is owed its mark wherever its tiles are drawn, and it draws
        // the control itself — compact of its own accord on a narrow screen.
        attributionControl: true,
      });
      const deckOverlay = new deck.MapboxOverlay({ interleaved: false, layers: [] });
      instance.addControl(deckOverlay);
      instance.on("click", (event) => {
        onPick.current?.({ lat: event.lngLat.lat, lng: event.lngLat.lng });
      });
      const reportView = () => {
        // Mapbox answers with nothing while the map has no size to speak of —
        // a container still laying out, or a tab that was never shown.
        const bounds = instance.getBounds();
        if (bounds === null) return;
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
      // A rider panning or zooming owns the camera until the next fit, so a
      // resize that happens to them — the URL bar folding away — does not
      // snatch it back. A map nobody has touched refits to its new size, which
      // is what keeps the route in frame across a rotation or a breakpoint.
      instance.on("movestart", (event) => {
        if (event.originalEvent !== undefined) touched.current = true;
      });
      instance.on("resize", () => {
        if (!touched.current) setResizes((count) => count + 1);
      });

      map.current = instance;
      overlay.current = deckOverlay;
      layers.current = deckLayers;
      setStatus("ready");
    });

    return () => {
      cancelled = true;
      if (frame.current !== undefined) cancelAnimationFrame(frame.current);
      frame.current = undefined;
      overlay.current?.finalize();
      map.current?.remove();
      map.current = null;
      overlay.current = null;
    };
  }, []);

  /** Every layer, as things stand this instant. Reads refs only, so it is stable. */
  const draw = React.useCallback(() => {
    const deckLayers = layers.current;
    const deckOverlay = overlay.current;
    if (deckLayers === null || deckOverlay === null) return;

    const current = scene.current;
    const still = reducedMotion.current;
    const now = performance.now();
    // A flash is timed against the wall clock its sighting was read from.
    // oxlint-disable-next-line effecttsgo/global-date
    const wall = Date.now();

    const cars = Array.from(
      glides.current.values(),
      (glide): CarAt => ({ position: glideAt(glide, now, still), heading: glide.heading }),
    );
    const flashes = (current.props.flashes ?? NONE).flatMap((flash): ReadonlyArray<FlashAt> => {
      const age = wall - flash.seenAtMs;
      return age >= 0 && age <= FLASH_MS ? [{ position: lngLat(flash.position), age }] : [];
    });
    // Under reduced motion a booking is a dot that comes and goes, with nothing
    // spreading from it.
    const rings = still
      ? []
      : flashes.flatMap((flash) =>
        RING_DELAYS.flatMap((delay): ReadonlyArray<RingAt> => {
          const progress = (flash.age - delay) / RING_MS;
          return progress >= 0 && progress <= 1 ? [{ position: flash.position, progress }] : [];
        })
      );
    const fade = (age: number) => 255 * Math.min(1, (FLASH_MS - age) / FLASH_FADE_MS);

    deckOverlay.setProps({
      layers: [
        new deckLayers.PolygonLayer<MapCell>({
          id: "cells",
          data: current.props.cells ?? NONE,
          getPolygon: (cell) => cell.boundary.map(([lng, lat]): [number, number] => [lng, lat]),
          getFillColor: (cell) =>
            (cell.heat ?? 0) > 0
              ? [200, 49, 42, Math.round(60 + 160 * (cell.heat ?? 0))]
              : [245, 158, 11, Math.round(20 + 170 * cell.weight)],
          getLineColor: [245, 158, 11, 90],
          lineWidthMinPixels: 1,
          stroked: true,
          filled: true,
          updateTriggers: { getFillColor: current.props.cells },
        }),
        new deckLayers.ScatterplotLayer<RingAt>({
          id: "flash-rings",
          data: rings,
          getPosition: (ring) => ring.position,
          radiusUnits: "pixels",
          getRadius: (ring) => 6 + 28 * easeOut(ring.progress),
          getFillColor: (ring) => rgba(YELLOW, 150 * (1 - ring.progress) ** 1.4),
          stroked: true,
          lineWidthUnits: "pixels",
          getLineWidth: 1,
          getLineColor: (ring) => rgba(INK, 70 * (1 - ring.progress)),
        }),
        new deckLayers.ScatterplotLayer<FlashAt>({
          id: "flash-cores",
          data: flashes,
          getPosition: (flash) => flash.position,
          radiusUnits: "pixels",
          getRadius: (flash) => 5 * (still ? 1 : easeOut(Math.min(1, flash.age / FLASH_POP_MS))),
          getFillColor: (flash) => rgba(YELLOW, fade(flash.age)),
          stroked: true,
          lineWidthUnits: "pixels",
          getLineWidth: 1.5,
          getLineColor: (flash) => rgba(INK, fade(flash.age)),
        }),
        new deckLayers.IconLayer<CarAt>({
          id: "city-cars",
          data: cars,
          getPosition: (car) => car.position,
          getIcon: () => ({ id: "car", url: CAR_ICON, width: 64, height: 64, anchorY: 32 }),
          // deck.gl turns counter-clockwise; a heading turns clockwise.
          getAngle: (car) => -car.heading,
          getSize: 22,
          sizeUnits: "pixels",
        }),
        // The route as a sign-yellow line on a black casing: yellow alone
        // vanishes against Positron's pale roads.
        new deckLayers.PathLayer<ReadonlyArray<MapPoint>>({
          id: "route-casing",
          data: current.path,
          getPath: (points) => points.map(lngLat),
          getColor: INK,
          widthMinPixels: 9,
          capRounded: true,
          jointRounded: true,
        }),
        new deckLayers.PathLayer<ReadonlyArray<MapPoint>>({
          id: "route",
          data: current.path,
          getPath: (points) => points.map(lngLat),
          getColor: YELLOW,
          widthMinPixels: 5,
          capRounded: true,
          jointRounded: true,
        }),
        new deckLayers.ScatterplotLayer<MapMarker>({
          id: "dots",
          data: current.props.dots ?? NONE,
          getPosition: (marker) => lngLat(marker.position),
          getFillColor: (marker) => DOT_COLOURS[marker.kind],
          radiusMinPixels: 2.5,
          radiusMaxPixels: 6,
          getRadius: 8,
          updateTriggers: { getFillColor: current.props.dots },
        }),
        new deckLayers.ScatterplotLayer<MapMarker>({
          id: "markers",
          data: current.plain,
          getPosition: (marker) => lngLat(marker.position),
          getFillColor: (marker) => DOT_COLOURS[marker.kind],
          getLineColor: INK,
          stroked: true,
          lineWidthMinPixels: 2,
          radiusMinPixels: 7,
          getRadius: 12,
        }),
        new deckLayers.IconLayer<MapMarker & { readonly kind: SignKind; }>({
          id: "signs",
          data: current.signs,
          getPosition: (marker) => lngLat(marker.position),
          getIcon: (marker) => ({
            id: marker.kind,
            url: SIGN_MARKERS[marker.kind],
            width: 64,
            height: 64,
            anchorY: 32,
          }),
          getSize: 30,
          sizeUnits: "pixels",
        }),
      ],
    });
  }, []);

  /** Draws every frame until `untilMs` (on the `performance.now()` clock), then stops. */
  const animate = React.useCallback((untilMs: number) => {
    animateUntil.current = Math.max(animateUntil.current, untilMs);
    if (frame.current !== undefined) return;
    const tick = () => {
      draw();
      frame.current = performance.now() < animateUntil.current
        ? requestAnimationFrame(tick)
        : undefined;
    };
    frame.current = requestAnimationFrame(tick);
  }, [draw]);

  // Everything that does not move by itself is drawn when it changes.
  React.useEffect(() => {
    if (status === "ready") draw();
  }, [status, draw, path, plain, signs, props.dots, props.cells]);

  // Each car glides from where it is drawn now to where it was just reported;
  // a car seen for the first time appears where it is.
  React.useEffect(() => {
    const now = performance.now();
    const next = new Map<string, Glide>();
    for (const car of props.cars ?? NONE) {
      const known = glides.current.get(car.key);
      const to = lngLat(car.position);
      next.set(car.key, {
        from: known === undefined ? to : glideAt(known, now, reducedMotion.current),
        to,
        heading: car.heading,
        startedAt: now,
      });
    }
    glides.current = next;
    if (status === "ready") animate(now + GLIDE_MS);
  }, [props.cars, status, animate]);

  // A new booking animates until its flash is over.
  React.useEffect(() => {
    if (status !== "ready") return;
    const flashes = props.flashes ?? NONE;
    if (flashes.length === 0) {
      draw();
      return;
    }
    // oxlint-disable-next-line effecttsgo/global-date
    const left = Math.max(...flashes.map((flash) => flash.seenAtMs)) + FLASH_MS - Date.now();
    animate(performance.now() + left);
  }, [props.flashes, status, animate, draw]);

  // String keys, so the effect fires when the points or the inset change rather
  // than when an array's identity does — a parent re-rendering with equal
  // points must not move the camera.
  const followKey = props.follow.map((point) => `${point.lat.toFixed(5)},${point.lng.toFixed(5)}`)
    .join("|");
  const inset = props.inset ?? EVEN;
  const insetKey = `${inset.top},${inset.right},${inset.bottom},${inset.left}`;

  React.useEffect(() => {
    const instance = map.current;
    if (status !== "ready" || instance === null || props.follow.length === 0) return;
    touched.current = false;

    if (props.follow.length === 1) {
      const [only] = props.follow;
      instance.easeTo({
        center: [only!.lng, only!.lat],
        zoom: Math.max(instance.getZoom(), 14),
        padding: inset,
      });
      return;
    }

    const lngs = props.follow.map((point) => point.lng);
    const lats = props.follow.map((point) => point.lat);
    instance.fitBounds(
      [[Math.min(...lngs), Math.min(...lats)], [Math.max(...lngs), Math.max(...lats)]],
      { padding: inset, maxZoom: 16, duration: 600 },
    );
    // `followKey` and `insetKey` stand in for `props.follow` and `inset`.
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [status, followKey, insetKey, resizes]);

  return (
    <div className={cn("relative min-h-64 flex-1 overflow-hidden bg-muted", props.className)}>
      {
        /* Positioned inline, not by class: Mapbox's stylesheet gives this
          element `position: relative`, and as unlayered CSS it outranks
          Tailwind's layered utilities — with the class alone the map collapses
          to no height at all. */
      }
      <div ref={container} style={{ position: "absolute", inset: 0 }} />
      {status !== "ready" && (
        <p className="text-muted-foreground absolute inset-0 grid place-items-center p-4 text-center text-sm">
          {status === "unsupported"
            ? "The map needs WebGL, which this browser does not provide."
            : status === "unconfigured"
            ? "The map needs a Mapbox token: set VITE_MAPBOX_TOKEN."
            : "Loading the map…"}
        </p>
      )}
    </div>
  );
};
