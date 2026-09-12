import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import Ionicons from "@expo/vector-icons/Ionicons";
import {
  Camera,
  Image as MapImage,
  Images,
  LineLayer,
  type MapState,
  MapView as MapboxMap,
  MarkerView,
  setAccessToken,
  setTelemetryEnabled,
  ShapeSource,
  SymbolLayer,
} from "@rnmapbox/maps";
import type { Feature, FeatureCollection, LineString, Point } from "geojson";
import * as React from "react";
import { View } from "react-native";
import Animated, {
  Easing,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withTiming,
} from "react-native-reanimated";

/**
 * The map, as the web's `SurgeMap` draws it: the trip's stops as sign panels,
 * the route in sign yellow, a camera that follows what matters — and, when a
 * screen asks for the live city, the free cars nearby and a yellow ring
 * wherever someone has just booked.
 *
 * Mapbox on both phones, against the same near-white basemap the web draws
 * with MapLibre — one picture of Amsterdam rather than Apple's on an iPhone
 * and Google's on an Android, and a pale one either way so the route's yellow
 * is the loudest thing on the screen. It needs a development build: the native
 * SDK is fetched at build time, which is more than Expo Go carries.
 *
 * The props are the web's, so a screen reads the same on both.
 *
 * The map is never the only way to do something. Every place a tap on it
 * chooses — a pickup, where a driver stands — has a list or a search beside
 * it, because a map is not something a screen reader can use. The cars and
 * the flashes are ambience, and hidden from it.
 */
export interface MapPoint {
  readonly lat: number;
  readonly lng: number;
}

export interface MapMarker {
  readonly id: string;
  readonly position: MapPoint;
  readonly kind: "pickup" | "dropoff" | "driver" | "self";
}

/** A car free to take a trip, pointing the way it is going. */
export interface MapCar {
  readonly key: string;
  readonly position: MapPoint;
  /** Degrees clockwise from north. */
  readonly heading: number;
}

/** Somewhere a trip was just booked, and when this map first heard of it. */
export interface MapFlash {
  readonly key: string;
  readonly position: MapPoint;
  readonly seenAtMs: number;
}

/** What the map shows, as the gateway's city feed takes it. */
export interface MapView {
  readonly west: number;
  readonly south: number;
  readonly east: number;
  readonly north: number;
  readonly zoom: number;
}

/**
 * The public token, which is what the app ships with and what every tile
 * request carries. The secret one beside it in `.env` is the build's, for
 * fetching the SDK, and never reaches a phone.
 */
// oxlint-disable-next-line effecttsgo/process-env
const MAPBOX_TOKEN = process.env.EXPO_PUBLIC_MAPBOX_TOKEN;

void setAccessToken(MAPBOX_TOKEN ?? null);

// Where a rider is going is the product's business and nobody else's; the map
// needs tiles, not a record of the journey.
setTelemetryEnabled(false);

/**
 * Mapbox's own pale basemap: near-white ground, grey streets, no colour of its
 * own worth the name. The web's OpenFreeMap positron is the same picture from
 * the other vendor, which is what keeps the two surfaces looking like one
 * product.
 */
const STYLE = "mapbox://styles/mapbox/light-v11";

const MARKER_TITLES: Record<MapMarker["kind"], string> = {
  pickup: "Pickup",
  dropoff: "Dropoff",
  driver: "Your driver",
  self: "You",
};

/**
 * Amsterdam, the one market: the centre, close enough that the city's free
 * cars are drawn from the first frame. Where the camera starts before there
 * is anything to follow.
 */
const AMSTERDAM = [4.8952, 52.3702];
const START_ZOOM = 12;

/**
 * What that opening camera sees, near enough for the gateway to start sending
 * a city. The map reports its own view the moment it settles; this is only
 * what the feed is asked for in the meantime.
 */
const AMSTERDAM_VIEW: MapView = {
  west: 4.83,
  south: 52.34,
  east: 4.96,
  north: 52.40,
  zoom: START_ZOOM,
};

/** Kept clear at each edge when a route is fitted: top, right, bottom, left. */
const EDGE = [48, 48, 48, 48];

/** How close the camera comes when it has one point to sit on. */
const FOLLOW_ZOOM = 14;

/** How long the camera takes to get anywhere, in milliseconds. */
const CAMERA_MS = 600;

/** How long a booking's ring runs. */
const FLASH_MS = 1800;

/** The image the car layer draws, and the size it is rendered at. */
const CAR_IMAGE = "surge-car";
const CAR_SIZE = 22;

/** One empty array rather than a new one per render, which the shape is keyed on. */
const NO_CARS: ReadonlyArray<MapCar> = [];

const toPosition = (point: MapPoint): [number, number] => [point.lng, point.lat];

/** The map's own account of what it shows, which is the city feed's viewport. */
const viewOf = (state: MapState): MapView | undefined => {
  const [east, north] = state.properties.bounds.ne;
  const [west, south] = state.properties.bounds.sw;
  if (east === undefined || north === undefined || west === undefined || south === undefined) {
    return undefined;
  }
  return { west, south, east, north, zoom: state.properties.zoom };
};

/**
 * A marker as the sheet's rail draws it: the pickup a yellow disc ringed in
 * ink, the dropoff an ink square with a yellow heart, the driver an ink disc
 * with a car — a car is a service, not a direction — and the device's own
 * position a yellow dot. Each sits on a white halo so it holds against any
 * street.
 */
const SignMarker = (props: { readonly kind: MapMarker["kind"]; }) => {
  if (props.kind === "self") {
    return <View className="size-4 rounded-full border-2 border-foreground bg-primary" />;
  }
  if (props.kind === "driver") {
    return (
      <View className="size-[30px] items-center justify-center rounded-full bg-white">
        <View className="size-[26px] items-center justify-center rounded-full bg-secondary">
          <Ionicons name="car" size={15} color="#ffffff" />
        </View>
      </View>
    );
  }
  return props.kind === "pickup"
    ? (
      <View className="size-[28px] items-center justify-center rounded-full bg-white">
        <View className="size-[22px] items-center justify-center rounded-full border-[3.5px] border-foreground bg-primary">
          <View className="size-[7px] rounded-full bg-foreground" />
        </View>
      </View>
    )
    : (
      <View className="size-[28px] items-center justify-center rounded-[9px] bg-white">
        <View className="size-[23px] items-center justify-center rounded-[7px] bg-foreground">
          <View className="size-[8px] rounded-[2.5px] bg-primary" />
        </View>
      </View>
    );
};

/**
 * The arrowhead a free car is drawn as, rendered once into an image the map
 * keeps: an ink triangle on a white disc. The layer below turns a copy of it
 * per car, which is how two hundred of them cost one draw rather than two
 * hundred views.
 *
 * The triangle is laid out rather than set in an icon font, because this view
 * is photographed the moment it mounts and a font that has not arrived yet
 * would be photographed as nothing.
 */
const CarImage = () => (
  <View
    className="items-center justify-center rounded-full bg-white"
    style={{ width: CAR_SIZE, height: CAR_SIZE }}
  >
    <View
      style={{
        width: 0,
        height: 0,
        borderLeftWidth: 5,
        borderRightWidth: 5,
        borderBottomWidth: 9,
        borderLeftColor: "transparent",
        borderRightColor: "transparent",
        borderBottomColor: colors.foreground,
      }}
    />
  </View>
);

/**
 * A booking: a yellow ring swelling out of a small ink-ringed dot and fading,
 * once. Under reduced motion the dot shows for the same moment, still.
 */
const FlashMarker = (props: { readonly flash: MapFlash; }) => {
  const reduced = useReducedMotion();
  const progress = useSharedValue(0);
  React.useEffect(() => {
    if (reduced) return;
    progress.value = withTiming(1, { duration: FLASH_MS, easing: Easing.out(Easing.cubic) });
  }, [progress, reduced]);
  const ring = useAnimatedStyle(() => ({
    opacity: 0.9 * (1 - progress.value),
    transform: [{ scale: 0.3 + progress.value * 1.7 }],
  }));
  const core = useAnimatedStyle(() => ({ opacity: 1 - progress.value * progress.value }));

  return (
    <MarkerView coordinate={toPosition(props.flash.position)} allowOverlap>
      <View
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
        pointerEvents="none"
        className="size-16 items-center justify-center"
      >
        <Animated.View
          className="absolute size-16 rounded-full border-[3px] border-primary bg-primary/20"
          style={ring}
        />
        <Animated.View
          className="size-3 rounded-full border-2 border-foreground bg-primary"
          style={core}
        />
      </View>
    </MarkerView>
  );
};

/**
 * The flashes still running. Each lives for its ring's length from when it was
 * first heard of; a clock ticks only while any is on the map.
 */
const Flashes = (props: { readonly flashes: ReadonlyArray<MapFlash>; }) => {
  // oxlint-disable-next-line effecttsgo/global-date
  const [now, setNow] = React.useState(() => Date.now());
  const running = props.flashes.length > 0;
  React.useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => {
      // oxlint-disable-next-line effecttsgo/global-date
      setNow(Date.now());
    }, 400);
    return () => {
      clearInterval(timer);
    };
  }, [running]);

  return (
    <>
      {props.flashes
        .filter((flash) => now - flash.seenAtMs < FLASH_MS)
        .map((flash) => <FlashMarker key={flash.key} flash={flash} />)}
    </>
  );
};

export const SurgeMap = (props: {
  readonly markers: ReadonlyArray<MapMarker>;
  readonly route: ReadonlyArray<MapPoint>;
  /** The points the camera keeps in view. One is centred on; several are fitted. */
  readonly follow: ReadonlyArray<MapPoint>;
  /** A tap on the map, when the screen wants one. */
  readonly onPick?: (point: MapPoint) => void;
  /** The city's free cars, from `useCity`. */
  readonly cars?: ReadonlyArray<MapCar>;
  /** Bookings just made, from `useCity`. */
  readonly flashes?: ReadonlyArray<MapFlash>;
  /** Told what the map shows whenever it settles, for the city feed. */
  readonly onView?: (view: MapView) => void;
  readonly className?: string;
}) => {
  // `Camera` names both the component and its ref, which is what it exports.
  const camera = React.useRef<Camera>(null);

  // Keyed by the points rather than the array, which is new on every render: the
  // camera moves when what it follows moves, not whenever the screen re-renders.
  const followKey = props.follow.map((point) => `${point.lat},${point.lng}`).join("|");
  const follow = React.useRef(props.follow);
  follow.current = props.follow;

  React.useEffect(() => {
    const points = follow.current;
    const [only] = points;
    if (only === undefined) return;
    if (points.length === 1) {
      camera.current?.setCamera({
        centerCoordinate: toPosition(only),
        zoomLevel: FOLLOW_ZOOM,
        animationDuration: CAMERA_MS,
      });
      return;
    }
    const lngs = points.map((point) => point.lng);
    const lats = points.map((point) => point.lat);
    camera.current?.fitBounds(
      [Math.max(...lngs), Math.max(...lats)],
      [Math.min(...lngs), Math.min(...lats)],
      EDGE,
      CAMERA_MS,
    );
  }, [followKey]);

  // The first view is the starting one; after that, wherever the map settles.
  const onView = props.onView;
  React.useEffect(() => {
    onView?.(AMSTERDAM_VIEW);
  }, [onView]);

  const cars = props.cars ?? NO_CARS;
  const carShape = React.useMemo(
    (): FeatureCollection<Point, { readonly heading: number; }> => ({
      type: "FeatureCollection",
      features: cars.map((car) => ({
        type: "Feature",
        id: car.key,
        geometry: { type: "Point", coordinates: toPosition(car.position) },
        properties: { heading: car.heading },
      })),
    }),
    [cars],
  );

  const path = props.route.map(toPosition);
  const routeShape: Feature<LineString> = {
    type: "Feature",
    geometry: { type: "LineString", coordinates: path },
    properties: {},
  };

  return (
    <View className={cn("flex-1 overflow-hidden bg-muted", props.className)}>
      <MapboxMap
        style={{ flex: 1 }}
        styleURL={STYLE}
        accessibilityLabel="Map"
        // A map that stays north-up and flat is one a route reads straight
        // off; tilting it is a thing to fiddle with, not a thing to use.
        rotateEnabled={false}
        pitchEnabled={false}
        compassEnabled={false}
        scaleBarEnabled={false}
        // Mapbox is owed its mark wherever its tiles are drawn. Lifted clear of
        // the sheet's top edge, which covers the last few points of the band.
        logoPosition={{ bottom: 34, left: 10 }}
        attributionPosition={{ bottom: 34, left: 78 }}
        onMapIdle={(state) => {
          const view = viewOf(state);
          if (view !== undefined) props.onView?.(view);
        }}
        onPress={(feature) => {
          const [lng, lat] = feature.geometry.coordinates;
          if (lng === undefined || lat === undefined) return;
          props.onPick?.({ lat, lng });
        }}
      >
        <Camera
          ref={camera}
          defaultSettings={{ centerCoordinate: AMSTERDAM, zoomLevel: START_ZOOM }}
        />
        <Images>
          <MapImage name={CAR_IMAGE}>
            <CarImage />
          </MapImage>
        </Images>
        {cars.length > 0 && (
          <ShapeSource id="cars" shape={carShape}>
            <SymbolLayer
              id="cars-arrows"
              style={{
                iconImage: CAR_IMAGE,
                iconRotate: ["get", "heading"],
                // Lying flat on the map, so a car turns with it as a car does.
                iconRotationAlignment: "map",
                iconAllowOverlap: true,
                iconIgnorePlacement: true,
              }}
            />
          </ShapeSource>
        )}
        {path.length > 1 && (
          // Yellow alone vanishes against a pale map; the black casing is what
          // carries it. Drawn after the cars, so the trip reads over the city.
          <ShapeSource id="route" shape={routeShape}>
            <LineLayer
              id="route-casing"
              style={{
                lineColor: colors.foreground,
                lineWidth: 9,
                lineCap: "round",
                lineJoin: "round",
              }}
            />
            <LineLayer
              id="route-line"
              style={{
                lineColor: colors.primary,
                lineWidth: 5,
                lineCap: "round",
                lineJoin: "round",
              }}
            />
          </ShapeSource>
        )}
        {props.flashes !== undefined && <Flashes flashes={props.flashes} />}
        {props.markers.map((marker) => (
          <MarkerView key={marker.id} coordinate={toPosition(marker.position)} allowOverlap>
            <View accessible accessibilityLabel={MARKER_TITLES[marker.kind]}>
              <SignMarker kind={marker.kind} />
            </View>
          </MarkerView>
        ))}
      </MapboxMap>
    </View>
  );
};
