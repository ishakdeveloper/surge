import { SurgeApi } from "@surge/domain/api/SurgeApi";
import type { Schema } from "effect";
import type { AST } from "effect/SchemaAST";
import { describe, expect, it } from "vitest";
import { SurgeApi as Generated } from "./generated.js";

/**
 * The answer to having declared the API twice.
 *
 * `proto/trip.proto` is the source of truth; grpc-gateway generates the REST
 * surface and its Swagger document from it, and `generated.ts` is that document
 * turned back into an `HttpApi` by `@effect/openapi-generator`. So one side of
 * this comparison is mechanically derived from the server, and the other is
 * `src/api/SurgeApi.ts` — written by hand, because the generated one cannot
 * express what makes a client worth having: proto3 leaves every field optional
 * in Swagger, ids arrive as bare strings rather than branded, and `int64` stays
 * the quoted string it is on the wire.
 *
 * Hand-written and derived-from-the-server is exactly the arrangement that
 * drifts. So this compares them: routes, methods, parameter names and the
 * field names reachable from each payload and response. What it deliberately
 * does not compare is *types* — that is the entire point of the hand-written
 * side, and a type mismatch surfaces immediately at runtime as a decode error
 * with a readable message. A route or a field that quietly disappeared surfaces
 * as a 404 in a browser at three in the morning.
 */

/** Every property name reachable from a schema, wrappers and nesting included. */
const fieldNames = (schema: Schema.Top | undefined): ReadonlyArray<string> => {
  if (schema === undefined) return [];

  const found = new Set<string>();
  const seen = new Set<AST>();

  const walk = (node: AST): void => {
    if (seen.has(node)) return;
    seen.add(node);

    // Only the four shapes that can contain a property name. Everything else is
    // a leaf as far as this comparison is concerned.
    if (node._tag === "Objects") {
      for (const property of node.propertySignatures) {
        found.add(String(property.name));
        walk(property.type);
      }
    } else if (node._tag === "Arrays") {
      for (const element of [...node.elements, ...node.rest]) walk(element);
    } else if (node._tag === "Union") {
      for (const member of node.types) walk(member);
    } else if (node._tag === "Suspend") {
      walk(node.thunk());
    }

    // A transformed schema — `Int64FromString`, a branded id — carries the shape
    // it decodes from underneath. Field names do not change across a transform,
    // but a `Schema.Class` puts its properties on the encoded side, so both
    // sides have to be walked or half the contract is invisible here.
    if (node.encoding !== undefined) {
      for (const link of node.encoding) walk(link.to);
    }
  };

  walk(schema.ast);
  return [...found].sort();
};

/**
 * The five properties this test reads, and no more.
 *
 * `HttpApi.Top` and `HttpApiEndpoint.Top` exist but are invariant in every type
 * parameter, so neither concrete API is assignable to them — and a test that
 * only walks a route table does not need to reproduce the type algebra of one.
 */
interface Endpoint {
  readonly method: string;
  readonly path: string;
  readonly params: Schema.Top | undefined;
  readonly query: Schema.Top | undefined;
  readonly payload: ReadonlyMap<string, { readonly schemas: ReadonlyArray<Schema.Top>; }>;
  readonly success: ReadonlySet<Schema.Top>;
}

interface Api {
  readonly groups: Record<string, { readonly endpoints: Record<string, Endpoint>; }>;
}

interface Declared {
  readonly params: ReadonlyArray<string>;
  readonly query: ReadonlyArray<string>;
  readonly payload: ReadonlyArray<string>;
  readonly success: ReadonlyArray<string>;
}

const describeEndpoint = (endpoint: Endpoint): Declared => ({
  params: fieldNames(endpoint.params),
  query: fieldNames(endpoint.query),
  payload: [...endpoint.payload.values()].flatMap((body) => body.schemas.flatMap(fieldNames))
    .sort(),
  success: [...endpoint.success].flatMap((schema) => fieldNames(schema)).sort(),
});

/** `groups` and `endpoints` are Records keyed by identifier, not arrays. */
const routes = (api: Api): ReadonlyMap<string, Declared> => {
  const found = new Map<string, Declared>();

  for (const group of Object.values(api.groups)) {
    for (const endpoint of Object.values(group.endpoints)) {
      found.set(`${endpoint.method} ${endpoint.path}`, describeEndpoint(endpoint));
    }
  }

  return found;
};

const ours = routes(SurgeApi);
const server = routes(Generated);

/**
 * Differences that are deliberate, each with the reason it exists. Anything not
 * on this list is drift.
 */
const KNOWN = {
  /**
   * The gateway binds cancellation twice — Google's `:cancel` custom-method
   * spelling and a plain path segment. Effect's router reads
   * `/v1/trips/:tripId:cancel` as a parameter named `tripId:cancel`, so the
   * client declares only the segment binding. Both reach the same handler.
   */
  routes: new Set(["POST /v1/trips/:tripId:cancel"]),
  /**
   * `idempotency_key` is a field on the proto message, and grpc-gateway copies
   * the `Idempotency-Key` header into it. The client sends the header, because
   * a key describes the request rather than the trip and a retrying proxy can
   * see a header without parsing a body.
   */
  payloadFields: new Set(["idempotencyKey"]),
} as const;

describe("the declared API matches what the gateway serves", () => {
  it("declares every route the server does", () => {
    const missing = [...server.keys()]
      .filter((route) => !ours.has(route) && !KNOWN.routes.has(route));

    expect(missing).toEqual([]);
  });

  it("declares no route the server does not serve", () => {
    const invented = [...ours.keys()].filter((route) => !server.has(route));
    expect(invented).toEqual([]);
  });

  it.each([...ours.keys()].map((route) => [route] as const))("agrees on %s", (route) => {
    const declared = ours.get(route);
    const actual = server.get(route);

    if (declared === undefined || actual === undefined) {
      throw new Error(`${route} is missing from one side, which the route tests cover`);
    }

    expect(declared.params).toEqual(actual.params);
    expect(declared.query).toEqual(actual.query);
    expect(declared.success).toEqual(actual.success);
    expect(declared.payload).toEqual(
      actual.payload.filter((field) => !KNOWN.payloadFields.has(field)),
    );
  });

  /**
   * Not drift, but the reason the hand-written side exists at all. If these ever
   * became true of the generated contract it could replace the hand-written one,
   * and this is what would say so.
   */
  it("is worth hand-writing", () => {
    const generatedTrip = server.get("GET /v1/trips/:tripId");
    expect(generatedTrip?.success).toContain("totalCents");

    // The generated schema types this as the string proto3 puts on the wire.
    // Ours decodes it to a number, which is the difference every call site
    // would otherwise have to know about.
    const ourTrip = ours.get("GET /v1/trips/:tripId");
    expect(ourTrip?.success).toContain("totalCents");
  });
});
