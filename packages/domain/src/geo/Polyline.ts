import { Result, Schema } from "effect";

/**
 * A point on a route, as decoded from the wire. Structurally a `Coordinate`, so
 * it can be handed to anything that takes one.
 */
export interface LatLng {
  readonly lat: number;
  readonly lng: number;
}

/**
 * The encoded shape was not a polyline this decoder can read.
 *
 * A typed failure rather than a throw: a route is decoded in render paths, and a
 * malformed one should become "no route drawn" at the call site's choosing, not
 * an exception that takes the page down with it.
 */
export class MalformedPolyline
  extends Schema.TaggedError<MalformedPolyline>()("MalformedPolyline", {
    reason: Schema.Literals(["Truncated", "Overflow"]),
  })
{}

/**
 * Decodes a polyline encoded at precision 6.
 *
 * Precision 6, not Google's 5: Valhalla encodes shapes at 1e-6 degrees, and a
 * route decoded at 5 looks plausible and is wrong by a factor of ten — a driver
 * rendered somewhere in Belgium. `backend/shared/geo` decodes the same way and
 * both are held to the same committed Valhalla fixture.
 *
 * Arithmetic rather than bit operators throughout. JavaScript's `<<` and `|`
 * truncate to 32 signed bits, and a sixth five-bit chunk shifted by thirty
 * lands in the sign bit; multiplying by powers of two does not.
 */
export const decodePolyline6 = (
  encoded: string,
): Result.Result<ReadonlyArray<LatLng>, MalformedPolyline> => {
  const points: Array<LatLng> = [];
  let index = 0;
  let lat = 0;
  let lng = 0;

  const next = (): number | MalformedPolyline => {
    let result = 0;
    let shift = 0;
    for (;;) {
      if (index >= encoded.length) return new MalformedPolyline({ reason: "Truncated" });
      const chunk = encoded.charCodeAt(index) - 63;
      index += 1;
      result += (chunk & 0x1f) * 2 ** shift;
      if (chunk < 0x20) break;
      shift += 5;
      if (shift > 60) return new MalformedPolyline({ reason: "Overflow" });
    }
    // Zig-zag: the lowest bit carries the sign.
    return result % 2 === 1 ? -(result + 1) / 2 : result / 2;
  };

  while (index < encoded.length) {
    const deltaLat = next();
    if (deltaLat instanceof MalformedPolyline) return Result.fail(deltaLat);
    const deltaLng = next();
    if (deltaLng instanceof MalformedPolyline) return Result.fail(deltaLng);

    lat += deltaLat;
    lng += deltaLng;
    points.push({ lat: lat / 1e6, lng: lng / 1e6 });
  }

  return Result.succeed(points);
};
