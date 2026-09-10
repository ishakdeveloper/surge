import { decodePolyline6, type LatLng } from "@surge/domain/geo/Polyline";
import { Result } from "effect";
import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The TypeScript half of the polyline contract.
 *
 * `backend/shared/geo/path_test.go` decodes this same committed Valhalla route,
 * and both hold the decoded shape to the length Valhalla reported. That is the
 * check that matters: a precision-6 polyline decoded at precision 5 is a
 * perfectly well-formed route that is wrong by a factor of ten, and nothing but
 * comparing its length to the router's own figure notices.
 */
const fixture = JSON.parse(
  fs.readFileSync(
    path.join(
      import.meta.dirname,
      "..",
      "..",
      "..",
      "..",
      "backend",
      "shared",
      "geo",
      "testdata",
      "route_centraal_rijksmuseum.json",
    ),
    "utf8",
  ),
) as { readonly shape: string; readonly valhallaKm: number; };

const haversineKm = (a: LatLng, b: LatLng): number => {
  const radians = (degrees: number) => (degrees * Math.PI) / 180;
  const dLat = radians(b.lat - a.lat);
  const dLng = radians(b.lng - a.lng);
  const h = Math.sin(dLat / 2) ** 2
    + Math.cos(radians(a.lat)) * Math.cos(radians(b.lat)) * Math.sin(dLng / 2) ** 2;
  return 2 * 6371.0088 * Math.asin(Math.sqrt(h));
};

describe("decodePolyline6", () => {
  it("decodes what Valhalla emits into a route as long as Valhalla says it is", () => {
    const decoded = decodePolyline6(fixture.shape);
    if (Result.isFailure(decoded)) throw new Error(`decode failed: ${decoded.failure.reason}`);
    const points = decoded.success;

    expect(points.length).toBeGreaterThan(50);

    // Generous bounds around the city. A precision error does not produce a
    // point slightly outside Amsterdam; it produces one in another country.
    for (const point of points) {
      expect(point.lat).toBeGreaterThan(52.27);
      expect(point.lat).toBeLessThan(52.45);
      expect(point.lng).toBeGreaterThan(4.72);
      expect(point.lng).toBeLessThan(5.08);
    }

    let km = 0;
    for (let index = 1; index < points.length; index += 1) {
      km += haversineKm(points[index - 1]!, points[index]!);
    }
    expect(Math.abs(km - fixture.valhallaKm)).toBeLessThan(fixture.valhallaKm * 0.02);
  });

  it("refuses a truncated polyline rather than returning a shorter route", () => {
    const decoded = decodePolyline6("y|~{bB_");
    expect(Result.isFailure(decoded)).toBe(true);
    if (Result.isFailure(decoded)) expect(decoded.failure.reason).toBe("Truncated");
  });

  it("decodes nothing to no points", () => {
    expect(decodePolyline6("")).toEqual(Result.succeed([]));
  });
});
