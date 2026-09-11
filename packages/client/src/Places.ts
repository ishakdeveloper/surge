import type { LatLng } from "@surge/domain/geo/Polyline";
import { Array as Arr, Config, Context, Effect, Layer, Option, pipe, Schema } from "effect";
import { HttpClient, HttpClientRequest, HttpClientResponse } from "effect/unstable/http";

/**
 * Places by name, and names for places: what lets a rider type "Rijksmuseum"
 * rather than click a map, and read "Stationsplein 41K" rather than a pair of
 * coordinates.
 *
 * Photon over OpenStreetMap, the data the basemap is drawn from, so a street
 * the map shows is a street the search finds. Platform-free like the rest of
 * this package: it asks for an `HttpClient` and each app provides its own.
 *
 * Kept inside Amsterdam. Surge serves one market, and a search that offered a
 * Damrak in another city would be offering trips the trip service refuses.
 */

/**
 * The search did not answer. Actionable, which is why it is typed: a rider
 * can still choose a place on the map or from the presets, and the screen says
 * so instead of leaving a field that silently finds nothing.
 */
export class PlacesUnavailable
  extends Schema.TaggedError<PlacesUnavailable>()("PlacesUnavailable", {})
{
  override readonly message = "Address search is not answering.";
}

export interface Place {
  /** What a rider would call it: a landmark's name, or a street and number. */
  readonly name: string;
  /** What tells two of the same name apart: the address, postcode and district. */
  readonly area: string;
  readonly position: LatLng;
}

/** The market, as the box every search is kept inside. */
const AMSTERDAM = { west: 4.72, south: 52.27, east: 5.08, north: 52.44 } as const;

/** Where results are ranked from when nothing better is known. Centraal, roughly. */
const CENTRE: LatLng = { lat: 52.3731, lng: 4.9003 };

/** Whether a point is inside the market — a device position outside it is no pickup. */
export const insideAmsterdam = (point: LatLng): boolean =>
  point.lng >= AMSTERDAM.west && point.lng <= AMSTERDAM.east
  && point.lat >= AMSTERDAM.south && point.lat <= AMSTERDAM.north;

const PhotonProperties = Schema.Struct({
  name: Schema.optionalKey(Schema.String),
  street: Schema.optionalKey(Schema.String),
  housenumber: Schema.optionalKey(Schema.String),
  postcode: Schema.optionalKey(Schema.String),
  district: Schema.optionalKey(Schema.String),
  city: Schema.optionalKey(Schema.String),
  osm_key: Schema.optionalKey(Schema.String),
}).annotate({ identifier: "PhotonProperties" });

const PhotonFeature = Schema.Struct({
  geometry: Schema.Struct({ coordinates: Schema.Tuple([Schema.Finite, Schema.Finite]) }),
  properties: PhotonProperties,
}).annotate({ identifier: "PhotonFeature" });

const PhotonFeatures = Schema.Struct({
  features: Schema.Array(PhotonFeature),
}).annotate({ identifier: "PhotonFeatures" });

/**
 * One result as a rider reads it, or nothing when it cannot be one.
 *
 * Water is dropped: OpenStreetMap names Amsterdam's canals, so "Damrak" finds
 * the canal beside the street, and a car cannot stop in either.
 */
const toPlace = (feature: typeof PhotonFeature.Type): Option.Option<Place> => {
  const { properties } = feature;
  if (properties.osm_key === "waterway") return Option.none();

  const address = properties.street === undefined
    ? undefined
    : properties.housenumber === undefined
    ? properties.street
    : `${properties.street} ${properties.housenumber}`;
  const name = properties.name ?? address;
  if (name === undefined) return Option.none();

  const [lng, lat] = feature.geometry.coordinates;
  const area = [
    name === address ? undefined : address,
    properties.postcode,
    properties.district ?? properties.city,
  ].filter((part): part is string => part !== undefined).join(", ");

  return Option.some({ name, area, position: { lat, lng } });
};

/**
 * Photon returns a long street once per segment. One row per name and area is
 * what a list a thumb picks from can use.
 */
const toPlaces = (features: typeof PhotonFeatures.Type): ReadonlyArray<Place> =>
  pipe(
    features.features,
    Arr.map(toPlace),
    Arr.getSomes,
    Arr.dedupeWith((a, b) => a.name === b.name && a.area === b.area),
  );

export interface PlacesService {
  /** Places matching what the rider typed, best first, inside Amsterdam. */
  readonly search: (query: string) => Effect.Effect<ReadonlyArray<Place>, PlacesUnavailable>;
  /** The place at a point: the nearest address, or nothing on open ground. */
  readonly at: (point: LatLng) => Effect.Effect<Option.Option<Place>, PlacesUnavailable>;
}

export class Places extends Context.Service<Places, PlacesService>()("Places") {
  static layer: Layer.Layer<Places, never, HttpClient.HttpClient> = Layer.effect(Places)(
    Effect.gen(function*() {
      const baseUrl = yield* Config.nonEmptyString("PLACES_URL").pipe(
        Config.withDefault("https://photon.komoot.io"),
      );
      const client = HttpClient.filterStatusOk(yield* HttpClient.HttpClient);

      const features = (request: HttpClientRequest.HttpClientRequest) =>
        client.execute(request.pipe(HttpClientRequest.acceptJson)).pipe(
          Effect.flatMap(HttpClientResponse.schemaBodyJson(PhotonFeatures)),
          Effect.map(toPlaces),
          Effect.catchTags({
            HttpClientError: () => new PlacesUnavailable(),
            SchemaError: () => new PlacesUnavailable(),
          }),
        );

      return {
        search: (query) =>
          features(
            HttpClientRequest.get(`${baseUrl}/api/`).pipe(
              HttpClientRequest.setUrlParams({
                q: query,
                lat: String(CENTRE.lat),
                lon: String(CENTRE.lng),
                bbox: `${AMSTERDAM.west},${AMSTERDAM.south},${AMSTERDAM.east},${AMSTERDAM.north}`,
                limit: "8",
              }),
            ),
          ),
        at: (point) =>
          features(
            HttpClientRequest.get(`${baseUrl}/reverse`).pipe(
              HttpClientRequest.setUrlParams({
                lat: String(point.lat),
                lon: String(point.lng),
                limit: "1",
              }),
            ),
          ).pipe(Effect.map(Arr.head)),
      };
    }),
    // A ConfigError means PLACES_URL is set to something unusable, and no
    // client behaviour recovers from not knowing where search lives.
  ).pipe(Layer.orDie);
}
