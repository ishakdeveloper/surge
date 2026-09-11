import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import Ionicons from "@expo/vector-icons/Ionicons";
import * as React from "react";
import { Platform, View } from "react-native";
import MapView, { Marker, Polyline, type Region } from "react-native-maps";

/**
 * The map, as the web's `SurgeMap` draws it: the trip's stops as sign panels,
 * the route in sign yellow, and a camera that follows what matters.
 *
 * Apple Maps on iOS and Google Maps on Android, through react-native-maps —
 * both in Expo Go without a key. The web draws with MapLibre; the props are the
 * same so a screen reads the same on both.
 *
 * The map is never the only way to do something. Every place a tap on it
 * chooses — a pickup, where a driver stands — has a list or a search beside
 * it, because a map is not something a screen reader can use.
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

const MARKER_TITLES: Record<MapMarker["kind"], string> = {
  pickup: "Pickup",
  dropoff: "Dropoff",
  driver: "Your driver",
  self: "You",
};

/** Amsterdam, the one market. Where the camera starts before there is anything to follow. */
const AMSTERDAM: Region = {
  latitude: 52.3676,
  longitude: 4.9041,
  latitudeDelta: 0.12,
  longitudeDelta: 0.12,
};

const EDGE = { top: 48, right: 48, bottom: 48, left: 48 };

const toLatLng = (point: MapPoint) => ({ latitude: point.lat, longitude: point.lng });

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

export const SurgeMap = (props: {
  readonly markers: ReadonlyArray<MapMarker>;
  readonly route: ReadonlyArray<MapPoint>;
  /** The points the camera keeps in view. One is centred on; several are fitted. */
  readonly follow: ReadonlyArray<MapPoint>;
  /** A tap on the map, when the screen wants one. */
  readonly onPick?: (point: MapPoint) => void;
  readonly className?: string;
}) => {
  const map = React.useRef<MapView>(null);

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

  const path = props.route.map(toLatLng);

  return (
    <View className={cn("flex-1 overflow-hidden bg-muted", props.className)}>
      <MapView
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
      </MapView>
    </View>
  );
};
