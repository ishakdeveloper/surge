import {
  Cancel200,
  Create200,
  Get200,
  Get404,
  GetPathParams,
  List200,
  List401,
  Preview200,
  SurgeApi,
} from "@surge/domain/api/SurgeApi";
import { Effect, Schema } from "effect";
import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";

/**
 * What can go wrong now that the client is generated.
 *
 * `proto/trip.proto` is the only place the API is declared, so route drift
 * cannot happen any more — the failure this replaced. What can happen instead is
 * that `scripts/generate-api-client.mjs` stops working silently. It patches the
 * OpenAPI document and then rewrites the generator's output by matching the
 * exact text it emits for a `format`; if the generator changes that text by a
 * space, every `replaceAll` no-ops, every id becomes a bare string, every
 * 64-bit field stays the quoted string proto3 puts on the wire, and nothing
 * fails until a page renders `"1627"` as a price.
 *
 * So this decodes captured responses from the running gateway — `testdata/`
 * holds real bytes, taken with `scripts/capture-api-fixtures.mjs` — and asserts
 * the three properties the generation exists to produce.
 */
const responses = JSON.parse(
  fs.readFileSync(path.join(import.meta.dirname, "testdata", "responses.json"), "utf8"),
) as Record<string, unknown>;

const decode = <A, I>(schema: Schema.Codec<A, I>, input: unknown) =>
  Effect.runSync(Schema.decodeUnknownEffect(schema)(input));

describe("the generated client decodes what the gateway sends", () => {
  it("previews", () => {
    const preview = decode(Preview200, responses["preview"]);

    expect(preview.fares.length).toBeGreaterThan(0);
    expect(preview.route.meters).toBeGreaterThan(0);
    expect(preview.route.polyline6.length).toBeGreaterThan(50);
  });

  it("books, lists and cancels", () => {
    const created = decode(Create200, responses["created"]);
    expect(created.trip.status).toBe("TRIP_STATUS_REQUESTED");

    const listed = decode(List200, responses["listed"]);
    expect(listed.trips.map((trip) => trip.id)).toContain(created.trip.id);
    expect(listed.nextPageToken).toBe("");
  });

  /**
   * The first of the three. `int64` is a *string* in proto3 JSON, because a JSON
   * number loses precision past 2^53, and the fixture below really does say
   * `"1627"`.
   */
  it("decodes 64-bit fields into numbers", () => {
    const preview = decode(Preview200, responses["preview"]);
    const raw = responses["preview"] as { fares: ReadonlyArray<{ totalCents: unknown; }>; };

    expect(raw.fares[0]?.totalCents).toBeTypeOf("string");
    expect(preview.fares[0]?.totalCents).toBeTypeOf("number");
    expect(preview.fares[0]?.totalCents).toBe(Number(raw.fares[0]?.totalCents));

    expect(decode(Get200, responses["created"]).trip.route.seconds).toBeTypeOf("number");
  });

  /**
   * The second. A `format` in the proto is how a string says what it is, and a
   * brand is how TypeScript stops it being any other string.
   */
  it("brands the ids", () => {
    // `.make` is only on a branded schema; a bare `Schema.String` has no such
    // constructor, so this failing to compile is the assertion.
    const params = GetPathParams.make({
      tripId: decode(Get200, responses["created"]).trip.id,
    });
    expect(params.tripId).toBe((responses["created"] as { trip: { id: string; }; }).trip.id);
  });

  /**
   * The third. proto3 has no required fields, so untouched the document makes
   * every property optional and `trip.id` is `string | undefined` everywhere.
   * The gateway marshals with `EmitUnpopulated`, which is why an unassigned
   * driver is `""` below rather than missing.
   */
  it("requires every field, because the gateway always sends one", () => {
    expect(decode(Get200, responses["created"]).trip.driverId).toBe("");

    const withoutDriver = structuredClone(responses["created"]) as {
      trip: Record<string, unknown>;
    };
    delete withoutDriver.trip["driverId"];

    expect(() => decode(Get200, withoutDriver)).toThrow();
  });

  /**
   * And the error shape, which the document was wrong about until the proto
   * declared it: `rest.go` installs a custom handler emitting
   * `{"error":{"code","message"}}`, while grpc-gateway's default said
   * `rpcStatus` — `{code: int, message, details}`. A client generated from that
   * would have failed to decode every error the server has ever sent.
   */
  it("decodes the error the gateway actually returns", () => {
    const notFound = decode(Get404, responses["notFound"]);
    expect(notFound.error.code).toBe("not_found");
    expect(notFound.error.message).toBe("unknown trip");

    expect(decode(List401, responses["unauth"]).error.code).toBe("unauthenticated");

    // The code is a closed set, so a server inventing one is a decode failure
    // here rather than a string nobody branches on.
    expect(() => decode(Get404, { error: { code: "kaput", message: "" } })).toThrow();
  });

  it("routes cancellation through the binding Effect can express", () => {
    const paths = Object.values(SurgeApi.groups)
      .flatMap((group) => Object.values(group.endpoints))
      .map((endpoint) => `${endpoint.method} ${endpoint.path}`);

    expect(paths).toContain("POST /v1/trips/:tripId/cancel");
    // Google's `:verb` spelling is in the proto and reaches the same handler,
    // but Effect's router reads it as a parameter named `tripId:cancel`.
    expect(paths).not.toContain("POST /v1/trips/:tripId:cancel");

    expect(decode(Cancel200, responses["created"]).trip.id).toBeTypeOf("string");
  });
});
