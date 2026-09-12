import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import Ionicons from "@expo/vector-icons/Ionicons";
import * as React from "react";
import { Platform, View } from "react-native";
import NativeMap, { Marker, Polyline, type Region } from "react-native-maps";
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
 * Apple Maps on iOS and Google Maps on Android, through react-native-maps —
 * both in Expo Go without a key. The web draws with MapLibre; the props are the
 * same so a screen reads the same on both.
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
const AMSTERDAM: Region = {
  latitude: 52.3702,
  longitude: 4.8952,
  latitudeDelta: 0.05,
  longitudeDelta: 0.05,
};

const EDGE = { top: 48, right: 48, bottom: 48, left: 48 };

/** How long a booking's ring runs. */
const FLASH_MS = 1800;

const toLatLng = (point: MapPoint) => ({ latitude: point.lat, longitude: point.lng });

/**
 * A region as the web's map reports its view. The zoom is MapLibre's, from
 * the longitude span across the map's width in 512-point tiles, so the
 * gateway's "cars from zoom 12" means the same distance on both.
 */
const viewOf = (region: Region, width: number): MapView => ({
  west: region.longitude - region.longitudeDelta / 2,
  east: region.longitude + region.longitudeDelta / 2,
  south: region.latitude - region.latitudeDelta / 2,
  north: region.latitude + region.latitudeDelta / 2,
  zoom: Math.log2((360 * width) / (512 * Math.max(region.longitudeDelta, 1e-6))),
});

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
 * A free car: an ink arrowhead on a white halo, turned the way it drives and
 * lying flat on the map, so it turns with the map too.
 */
const CarMarker = React.memo((props: { readonly car: MapCar; }) => (
  <Marker
    coordinate={toLatLng(props.car.position)}
    anchor={{ x: 0.5, y: 0.5 }}
    rotation={props.car.heading}
    flat
    tappable={false}
    tracksViewChanges={false}
  >
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className="size-[22px] items-center justify-center rounded-full bg-white"
      style={{
        shadowColor: "#000000",
        shadowOpacity: 0.18,
        shadowRadius: 3,
        shadowOffset: { width: 0, height: 1 },
      }}
    >
      {/* Ionicons' arrow points north-east; turned back a quarter, it points north. */}
      <Ionicons
        name="navigate"
        size={13}
        color={colors.foreground}
        style={{ transform: [{ rotate: "-45deg" }] }}
      />
    </View>
  </Marker>
));

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
    <Marker
      coordinate={toLatLng(props.flash.position)}
      anchor={{ x: 0.5, y: 0.5 }}
      tappable={false}
      // Google's markers are pictures of their views, retaken only while this
      // is on; Apple's are live views and animate on their own.
      tracksViewChanges={Platform.OS === "android"}
    >
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
    </Marker>
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
  const map = React.useRef<NativeMap>(null);
  const width = React.useRef(390);

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
      map.current?.animateToRegion({
        ...toLatLng(only),
        latitudeDelta: 0.02,
        longitudeDelta: 0.02,
      });
    } else {
      map.current?.fitToCoordinates(points.map(toLatLng), { edgePadding: EDGE, animated: true });
    }
  }, [followKey]);

  // The first view is the starting one; after that, wherever the map settles.
  const onView = props.onView;
  React.useEffect(() => {
    onView?.(viewOf(AMSTERDAM, width.current));
  }, [onView]);

  const path = props.route.map(toLatLng);

  return (
    <View
      className={cn("flex-1 overflow-hidden bg-muted", props.className)}
      onLayout={(event) => {
        width.current = event.nativeEvent.layout.width;
      }}
    >
      <NativeMap
        ref={map}
        style={{ flex: 1 }}
        initialRegion={AMSTERDAM}
        userInterfaceStyle="light"
        // Apple's desaturated style, so the route's sign yellow is the loudest
        // thing on the map. Google's has no equivalent without a key.
        mapType={Platform.OS === "ios" ? "mutedStandard" : "standard"}
        toolbarEnabled={false}
        showsPointsOfInterests={false}
        accessibilityLabel="Map"
        onRegionChangeComplete={(region) => {
          props.onView?.(viewOf(region, width.current));
        }}
        onPress={(event) => {
          // Android reports a tap on a marker as a tap on the map as well.
          if (event.nativeEvent.action === "marker-press") {
            return;
          }
          props.onPick?.({
            lat: event.nativeEvent.coordinate.latitude,
            lng: event.nativeEvent.coordinate.longitude,
          });
        }}
      >
        {props.cars?.map((car) => <CarMarker key={car.key} car={car} />)}
        {props.flashes !== undefined && <Flashes flashes={props.flashes} />}
        {path.length > 1 && (
          <>
            {/* Yellow alone vanishes against a pale map; the black casing is what carries it. */}
            <Polyline
              coordinates={path}
              strokeColor={colors.foreground}
              strokeWidth={9}
              lineCap="round"
              lineJoin="round"
            />
            <Polyline
              coordinates={path}
              strokeColor={colors.primary}
              strokeWidth={5}
              lineCap="round"
              lineJoin="round"
            />
          </>
        )}
        {props.markers.map((marker) => (
          <Marker
            key={marker.id}
            coordinate={toLatLng(marker.position)}
            anchor={{ x: 0.5, y: 0.5 }}
            title={MARKER_TITLES[marker.kind]}
            tracksViewChanges={false}
          >
            <SignMarker kind={marker.kind} />
          </Marker>
        ))}
      </NativeMap>
    </View>
  );
};
