import type { Coordinate } from "@surge/domain/trip/Trip";

/**
 * Trips worth one tap, on both apps. Real Amsterdam addresses on roads, so a
 * demo does not begin with finding somewhere a car can actually stop — and the
 * accessible way to choose a trip, since a map is not one.
 */
export interface RidePreset {
  readonly label: string;
  readonly pickup: Coordinate;
  readonly dropoff: Coordinate;
}

export const RIDE_PRESETS: ReadonlyArray<RidePreset> = [
  {
    label: "Centraal → Rijksmuseum",
    pickup: { lat: 52.3791, lng: 4.9003 },
    dropoff: { lat: 52.36, lng: 4.8852 },
  },
  {
    label: "Sloterdijk → Westerpark",
    pickup: { lat: 52.3889, lng: 4.8377 },
    dropoff: { lat: 52.3868, lng: 4.8752 },
  },
  {
    label: "Zuid → De Pijp",
    pickup: { lat: 52.3389, lng: 4.8723 },
    dropoff: { lat: 52.3533, lng: 4.8946 },
  },
];

/** Places a driver can stand, near the rider presets' pickups. */
export const DRIVER_SPOTS: ReadonlyArray<
  { readonly label: string; readonly position: Coordinate; }
> = [
  { label: "Centraal", position: { lat: 52.3786, lng: 4.8996 } },
  { label: "Sloterdijk", position: { lat: 52.3882, lng: 4.8386 } },
  { label: "Zuid", position: { lat: 52.3395, lng: 4.8731 } },
];
