import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import * as React from "react";
import { View } from "react-native";
import MapView, { Marker, Polyline, type Region } from "react-native-maps";

/**
 * The map, as the web's `SurgeMap` draws it: markers by kind, the route, and a
 * camera that follows what matters.
 *
 * Apple Maps on iOS and Google Maps on Android, through react-native-maps —
 * both in Expo Go without a key. The web draws with MapLibre; the props are the
 * same so a screen reads the same on both.
 *
 * The map is never the only way to do something. Every place a tap on it
 * chooses — a pickup, where a driver stands — has a list beside it, because a
 * map is not something a screen reader can use.
 */
export interface MapPoint {
  readonly lat: number;
  readonly lng: number;
}

export interface MapMarker {
  readonly id: string;
  readonly position: MapPoint;
  /** Categorisation, which is the only thing colour is used for — the web's palette. */
  readonly kind: "pickup" | "dropoff" | "driver" | "self";
}

const MARKER_COLORS: Record<MapMarker["kind"], string> = {
  pickup: "rgb(34, 197, 94)",
  dropoff: "rgb(96, 165, 250)",
  driver: "rgb(245, 158, 11)",
  self: "rgb(240, 240, 240)",
};

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

  return (
    <View className={cn("h-72 overflow-hidden rounded-xl border border-border", props.className)}>
      <MapView
        ref={map}
        style={{ flex: 1 }}
        initialRegion={AMSTERDAM}
        userInterfaceStyle="dark"
        toolbarEnabled={false}
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
        {props.route.length > 1 && (
          <Polyline
            coordinates={props.route.map(toLatLng)}
            strokeColor={colors.primary}
            strokeWidth={4}
          />
        )}
        {props.markers.map((marker) => (
          <Marker
            key={marker.id}
            coordinate={toLatLng(marker.position)}
            pinColor={MARKER_COLORS[marker.kind]}
            title={MARKER_TITLES[marker.kind]}
          />
        ))}
      </MapView>
    </View>
  );
};
