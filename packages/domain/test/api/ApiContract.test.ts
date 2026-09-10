import { SurgeApi } from "@surge/domain/api/SurgeApi";
import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The answer to having declared the API twice.
 *
 * The Go gateway's REST surface is generated from `proto/trip.proto`, and
 * `packages/domain/src/api/SurgeApi.ts` declares the same surface again so
 * TypeScript gets a typed client and runtime decoding. Two declarations of one
 * thing drift — that is the whole reason the Go side stopped hand-writing its
 * REST handlers — so this reads the generated OpenAPI document and fails when
 * they disagree.
 *
 * It compares routes rather than schemas. A field type mismatch surfaces
 * immediately at runtime as a decode error with a readable message, which is
 * exactly what Effect Schema is for; a *missing route* surfaces as a 404 in a
 * browser at three in the morning.
 */
const specPath = path.join(
  import.meta.dirname,
  "..",
  "..",
  "..",
  "..",
  "docs",
  "api",
  "surge.swagger.json",
);

interface OpenApiDocument {
  readonly paths: Record<string, Record<string, unknown>>;
}

/** OpenAPI writes `{tripId}`; Effect writes `:tripId`. */
const normalise = (openApiPath: string): string => openApiPath.replaceAll(/\{(\w+)\}/g, ":$1");

const declaredRoutes = (): ReadonlyArray<string> => {
  const routes: Array<string> = [];

  // Both are records keyed by identifier rather than arrays, which is what
  // makes `api.groups.trips.endpoints.preview` a typed lookup elsewhere.
  for (const group of Object.values(SurgeApi.groups)) {
    for (const endpoint of Object.values(group.endpoints)) {
      routes.push(`${endpoint.method} ${endpoint.path}`);
    }
  }

  return routes.sort();
};

const servedRoutes = (): ReadonlyArray<string> => {
  const document = JSON.parse(fs.readFileSync(specPath, "utf8")) as OpenApiDocument;
  const routes: Array<string> = [];

  for (const [openApiPath, operations] of Object.entries(document.paths)) {
    for (const method of Object.keys(operations)) {
      routes.push(`${method.toUpperCase()} ${normalise(openApiPath)}`);
    }
  }

  return routes.sort();
};

describe("the declared API matches what the gateway serves", () => {
  it("finds the generated document at all", () => {
    // A missing spec would make every assertion below vacuously pass, which is
    // the worst possible outcome for a drift test.
    expect(fs.existsSync(specPath), `no OpenAPI document at ${specPath}; run \`make proto\``).toBe(
      true,
    );
    expect(servedRoutes().length).toBeGreaterThan(0);
  });

  it("declares no route the gateway does not serve", () => {
    const served = new Set(servedRoutes());
    const phantom = declaredRoutes().filter((route) => !served.has(route));

    expect(phantom, "these would 404 at runtime").toEqual([]);
  });

  it("declares every route the gateway serves, or explains why not", () => {
    // `:cancel` is Google's convention for a custom method and the gateway
    // serves it, but Effect's router reads `/v1/trips/:tripId:cancel` as a
    // parameter literally named "tripId:cancel". The proto carries a second
    // binding at /v1/trips/{trip_id}/cancel for that reason, and this is the
    // one route we knowingly do not declare.
    const knownUndeclared = new Set(["POST /v1/trips/:tripId:cancel"]);

    const declared = new Set(declaredRoutes());
    const missing = servedRoutes()
      .filter((route) => !declared.has(route))
      .filter((route) => !knownUndeclared.has(route));

    expect(missing, "the gateway serves these and the client cannot reach them").toEqual([]);
  });
});
