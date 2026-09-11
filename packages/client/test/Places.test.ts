import { Places, PlacesUnavailable } from "@/Places.js";
import { describe, expect, it } from "@effect/vitest";
import { Effect, Layer, Option, Ref } from "effect";
import { HttpClient, HttpClientResponse, UrlParams } from "effect/unstable/http";

/** A Photon GeoJSON feature, with the keys Photon sends that nothing here reads. */
const feature = (properties: Record<string, string>, lng: number, lat: number) => ({
  type: "Feature",
  geometry: { type: "Point", coordinates: [lng, lat] },
  properties: { osm_type: "N", osm_id: 1, country: "Netherlands", ...properties },
});

const collection = (...features: ReadonlyArray<ReturnType<typeof feature>>) =>
  JSON.stringify({ type: "FeatureCollection", features });

const RIJKSMUSEUM = feature(
  {
    name: "Rijksmuseum",
    street: "Museumstraat",
    housenumber: "1",
    postcode: "1071 XX",
    district: "Zuid",
    city: "Amsterdam",
    osm_key: "tourism",
  },
  4.8852,
  52.36,
);

const STATIONSPLEIN = feature(
  {
    street: "Stationsplein",
    housenumber: "41K",
    postcode: "1012 AB",
    district: "Centrum",
    city: "Amsterdam",
    osm_key: "place",
  },
  4.9002,
  52.3792,
);

/**
 * The service over a stub Photon that answers every request with one response
 * and remembers what it was asked, query string included.
 */
const withPhoton = (response: { readonly status: number; readonly body: string; }) =>
  Effect.gen(function*() {
    const requests = yield* Ref.make<ReadonlyArray<string>>([]);

    const client = HttpClient.make((request) =>
      Ref.update(requests, (all) => [
        ...all,
        `${request.url}?${UrlParams.toString(request.urlParams)}`,
      ]).pipe(
        Effect.as(HttpClientResponse.fromWeb(
          request,
          new Response(response.body, {
            status: response.status,
            headers: { "content-type": "application/json" },
          }),
        )),
      )
    );

    const places = yield* Effect.provide(
      Places,
      Places.layer.pipe(Layer.provide(Layer.succeed(HttpClient.HttpClient)(client))),
    );

    return { places, requests } as const;
  });

describe("Places", () => {
  it.effect("names a landmark by its name and an address by its street and number", () =>
    Effect.gen(function*() {
      const { places } = yield* withPhoton({
        status: 200,
        body: collection(RIJKSMUSEUM, STATIONSPLEIN),
      });

      expect(yield* places.search("rijks")).toEqual([
        {
          name: "Rijksmuseum",
          area: "Museumstraat 1, 1071 XX, Zuid",
          position: { lat: 52.36, lng: 4.8852 },
        },
        {
          name: "Stationsplein 41K",
          area: "1012 AB, Centrum",
          position: { lat: 52.3792, lng: 4.9002 },
        },
      ]);
    }));

  it.effect("drops canals, and a street Photon returns once per segment", () =>
    Effect.gen(function*() {
      // What "Damrak" really returns: the canal, then the street in pieces.
      const { places } = yield* withPhoton({
        status: 200,
        body: collection(
          feature({ name: "Damrak", postcode: "1012 AC", osm_key: "waterway" }, 4.8987, 52.3768),
          feature({ name: "Damrak", postcode: "1012 JS", district: "Centrum" }, 4.8958, 52.3745),
          feature({ name: "Damrak", postcode: "1012 JS", district: "Centrum" }, 4.8961, 52.375),
        ),
      });

      const found = yield* places.search("damrak");

      expect(found.map((place) => `${place.name} · ${place.area}`)).toEqual([
        "Damrak · 1012 JS, Centrum",
      ]);
    }));

  it.effect("searches inside Amsterdam and nowhere else", () =>
    Effect.gen(function*() {
      const { places, requests } = yield* withPhoton({ status: 200, body: collection() });

      yield* places.search("damrak");

      const [asked] = yield* Ref.get(requests);
      expect(asked).toContain("/api/?");
      expect(asked).toContain("q=damrak");
      expect(asked).toContain("bbox=4.72%2C52.27%2C5.08%2C52.44");
    }));

  it.effect("names a point by the nearest address, and open ground by nothing", () =>
    Effect.gen(function*() {
      const found = yield* withPhoton({ status: 200, body: collection(STATIONSPLEIN) });
      const here = yield* found.places.at({ lat: 52.3791, lng: 4.9003 });

      expect(Option.map(here, (place) => place.name)).toEqual(Option.some("Stationsplein 41K"));
      expect(yield* Ref.get(found.requests)).toEqual([
        expect.stringContaining("/reverse?lat=52.3791&lon=4.9003"),
      ]);

      const empty = yield* withPhoton({ status: 200, body: collection() });
      expect(yield* empty.places.at({ lat: 52.3, lng: 4.8 })).toEqual(Option.none());
    }));

  it.effect("reports a search that does not answer as PlacesUnavailable", () =>
    Effect.gen(function*() {
      const { places } = yield* withPhoton({ status: 502, body: "Bad Gateway" });

      expect(yield* Effect.flip(places.search("damrak"))).toBeInstanceOf(PlacesUnavailable);
    }));

  it.effect("and one that answers with something other than places", () =>
    Effect.gen(function*() {
      const { places } = yield* withPhoton({ status: 200, body: "<html>maintenance</html>" });

      expect(yield* Effect.flip(places.search("damrak"))).toBeInstanceOf(PlacesUnavailable);
    }));
});
